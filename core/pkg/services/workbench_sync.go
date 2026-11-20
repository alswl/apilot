package services

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type workbenchReader func(context.Context, common.WorkbenchConnection) ([]common.WorkbenchTask, error)

type workbenchStore interface {
	WorkbenchConnections() ([]common.WorkbenchSnapshot, error)
	SaveWorkbench(common.WorkbenchConnection, bool) error
	DeleteWorkbench(string) error
	CommitWorkbenchImport(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, *time.Time) error
	RecordWorkbench(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, *time.Time) error
	WorkbenchPage(common.WorkbenchQuery) (common.WorkbenchPage, error)
	WorkbenchObject(string) (common.WorkbenchTask, error)
	WorkbenchObjects(string) ([]common.WorkbenchTask, error)
}

func (s *Service) workbenchStore() (workbenchStore, error) {
	st, ok := s.store.(workbenchStore)
	if !ok {
		return nil, common.Err("INTERNAL", "Workbench storage unavailable")
	}
	return st, nil
}

func (s *Service) WorkbenchConnections() ([]common.WorkbenchSnapshot, error) {
	st, err := s.workbenchStore()
	if err != nil {
		return nil, err
	}
	connections, err := st.WorkbenchConnections()
	if err != nil {
		return nil, err
	}
	for i := range connections {
		enabled, err := s.workbenchPluginEnabled(connections[i].Kind)
		if err != nil {
			return nil, err
		}
		connections[i].PluginDisabled = !enabled
		if attempts := connections[i].Attempts; len(attempts) > 0 && attempts[0].State == "running" {
			if progress, ok := s.readProgress.Load(attempts[0].ID); ok {
				value := progress.(common.WorkbenchReadProgress)
				attempts[0].Progress = &value
			}
		}
		connections[i].Capabilities = s.workbenchConnectionCapabilities(connections[i])
	}
	return connections, nil
}

func (s *Service) WorkbenchSnapshot(id string) (common.WorkbenchSnapshot, error) {
	list, err := s.WorkbenchConnections()
	if err != nil {
		return common.WorkbenchSnapshot{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return common.WorkbenchSnapshot{}, common.Err("NOT_FOUND", "Unknown workbench connection")
}

func validWorkbenchState(state string) bool {
	switch state {
	case "open", "in_progress", "done", "cancelled", "unknown":
		return true
	}
	return false
}

func (s *Service) WorkbenchTasks(q common.WorkbenchQuery) (common.WorkbenchPage, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 0 || q.Limit > 100 || q.Offset < 0 || q.Revision < 0 || (q.State != "" && !validWorkbenchState(q.State)) {
		return common.WorkbenchPage{}, common.Err("INVALID_ARGUMENT", "Invalid workbench query")
	}
	st, err := s.workbenchStore()
	if err != nil {
		return common.WorkbenchPage{}, err
	}
	page, err := st.WorkbenchPage(q)
	if err != nil || len(page.Items) == 0 {
		return page, err
	}
	connections, err := st.WorkbenchConnections()
	if err != nil {
		return common.WorkbenchPage{}, err
	}
	versions := make(map[string]int64, len(connections))
	for _, connection := range connections {
		versions[connection.ID] = connection.Version
	}
	for i := range page.Items {
		task := &page.Items[i]
		source := *task
		source.WorkbenchLabels = nil
		task.TargetVersion = workbenchTaskTargetVersion(source)
		task.ConnectionVersion = versions[task.ConnectionID]
	}
	return page, nil
}

func (s *Service) WorkbenchTask(id string) (common.WorkbenchTask, error) {
	st, err := s.workbenchStore()
	if err != nil {
		return common.WorkbenchTask{}, err
	}
	task, err := st.WorkbenchObject(id)
	if err != nil {
		return task, err
	}
	connection, err := s.WorkbenchSnapshot(task.ConnectionID)
	if err != nil {
		return common.WorkbenchTask{}, err
	}
	task.TargetVersion = workbenchTaskTargetVersion(task)
	task.ConnectionVersion = connection.Version
	if metadata, ok := s.store.(interface {
		WorkbenchTaskLabels([]string) (map[string][]common.WorkbenchLabel, error)
	}); ok {
		labels, err := metadata.WorkbenchTaskLabels([]string{id})
		if err != nil {
			return common.WorkbenchTask{}, err
		}
		task.WorkbenchLabels = labels[id]
	}
	return task, nil
}

func (s *Service) SaveWorkbench(c common.WorkbenchConnection) (string, error) {
	return s.ConfigureWorkbench(c, common.WorkbenchBridgeInput{})
}

func (s *Service) ConfigureWorkbench(c common.WorkbenchConnection, input common.WorkbenchBridgeInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return "", common.Err("CONFLICT", "App is stopping")
	}
	st, err := s.workbenchStore()
	if err != nil {
		return "", err
	}
	if err := s.workbenchPluginAvailable(c.Kind); err != nil {
		return "", err
	}
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" || len(c.Name) > 256 {
		return "", common.Err("INVALID_ARGUMENT", "Connection name is required")
	}
	switch c.Kind {
	case common.WorkbenchObsidianTasks:
		if !filepath.IsAbs(c.Vault) || (c.Scope != "" && !filepath.IsLocal(c.Scope)) {
			return "", common.Err("INVALID_ARGUMENT", "Choose an absolute vault path and a relative scope")
		}
		if c.ReadMethod != "" && c.ReadMethod != common.WorkbenchObsidianCLI {
			return "", common.Err("INVALID_ARGUMENT", "Unknown Obsidian read method")
		}
		c.Mode = "local"
		c.Vault = filepath.Clean(c.Vault)
		c.Scope = filepath.Clean(c.Scope)
		c.Repository = ""
		for symbol, state := range c.Statuses {
			if len([]rune(symbol)) != 1 || strings.TrimSpace(symbol) == "" || strings.ContainsAny(symbol, "[]") || strings.EqualFold(symbol, "x") || !validWorkbenchState(state) {
				return "", common.Err("INVALID_ARGUMENT", "Invalid Obsidian status mapping")
			}
		}
	case common.WorkbenchGitHubIssues:
		if c.ReadMethod != "" {
			return "", common.Err("INVALID_ARGUMENT", "Read method requires an Obsidian connection")
		}
		if !strings.EqualFold(c.Repository, "alswl/issues") {
			return "", common.Err("INVALID_ARGUMENT", "Mock repository must be alswl/issues")
		}
		c.Repository, c.Mode = "alswl/issues", "mock"
		c.Vault, c.Scope, c.Statuses = "", "", nil
	default:
		return "", common.Err("INVALID_ARGUMENT", "Unknown connector type")
	}
	connections, err := st.WorkbenchConnections()
	if err != nil {
		return "", err
	}
	for _, existing := range connections {
		if existing.ID != c.ID && strings.EqualFold(existing.Name, c.Name) {
			return "", common.Err("INVALID_ARGUMENT", "Connection name must be unique")
		}
	}
	reset := false
	var prior common.WorkbenchConnection
	trigger := "saved"
	if c.ID == "" {
		c.ID = common.ID()
		c.Version = 1
	} else {
		old, err := s.WorkbenchSnapshot(c.ID)
		if err != nil {
			return "", err
		}
		if old.Kind != c.Kind {
			return "", common.Err("INVALID_ARGUMENT", "Connector type cannot change")
		}
		if c.Version != old.Version {
			return "", common.Err("STALE_REVISION", "Connection configuration changed")
		}
		prior = old.WorkbenchConnection
		c.Paused = old.Paused
		reset = c.Vault != old.Vault || c.Scope != old.Scope || c.Repository != old.Repository || !maps.Equal(c.Statuses, old.Statuses)

	}
	// Connector verification may contact an external process or bridge. Keep it
	// outside the service lock, then revalidate all configuration assumptions.
	s.wg.Add(1)
	s.mu.Unlock()
	bridge, err := s.prepareWorkbenchBridge(&c, prior, input)
	s.mu.Lock()
	s.wg.Done()
	if s.stopping {
		return "", common.Err("CONFLICT", "App is stopping")
	}
	if err != nil {
		return "", err
	}
	if err := s.workbenchPluginAvailable(c.Kind); err != nil {
		return "", err
	}
	connections, err = st.WorkbenchConnections()
	if err != nil {
		return "", err
	}
	foundPrior := prior.ID == ""
	for _, current := range connections {
		if current.ID == prior.ID {
			foundPrior = true
			if !reflect.DeepEqual(current.WorkbenchConnection, prior) {
				return "", common.Err("STALE_REVISION", "Connection configuration changed during verification")
			}
		} else if current.ID != c.ID && strings.EqualFold(current.Name, c.Name) {
			return "", common.Err("INVALID_ARGUMENT", "Connection name must be unique")
		}
	}
	if !foundPrior {
		return "", common.Err("STALE_REVISION", "Connection was removed during verification")
	}
	changed := reset || prior.ReadMethod != c.ReadMethod || prior.WriteEnabled != c.WriteEnabled || bridge != nil
	if prior.ID != "" && changed {
		c.Version++
		trigger = "configuration"
	}
	if bridge != nil {
		err = s.store.(workbenchBridgeStore).SaveWorkbenchBridge(c, reset, bridge)
	} else {
		err = st.SaveWorkbench(c, reset)
	}
	if err != nil {
		return "", err
	}
	if changed && s.workbenchActive[c.ID] != nil {
		s.workbenchActive[c.ID]()
	}

	if c.Paused {
		return c.ID, nil
	}
	return c.ID, s.startWorkbenchLocked(st, c.ID, trigger)
}

func (s *Service) SyncWorkbench(id string) error {
	_, err := s.RunWorkbenchSync(id)
	return err
}

func (s *Service) RunWorkbenchSync(id string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.workbenchStore()
	if err != nil {
		return 0, err
	}
	if err := s.startWorkbenchLocked(st, id, "manual"); err != nil {
		return 0, err
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return 0, err
	}
	if len(snapshot.Attempts) == 0 {
		return 0, common.Err("INTERNAL", "Sync attempt was not recorded")
	}
	return snapshot.Attempts[0].ID, nil
}

func (s *Service) workbenchReadTimeout(c common.WorkbenchConnection) time.Duration {
	if c.Kind == common.WorkbenchObsidianTasks && s.obsidianSync.ScanTimeoutConfigured {
		return s.obsidianSync.ScanTimeout
	}
	if c.ReadMethod == common.WorkbenchObsidianCLI {
		return 90 * time.Second
	}
	return 30 * time.Second
}

func (s *Service) startWorkbenchLocked(st workbenchStore, id, trigger string) error {
	return s.startWorkbenchPathsLocked(st, id, trigger, nil)
}

func (s *Service) startWorkbenchPathsLocked(st workbenchStore, id, trigger string, paths []string) error {
	if s.stopping {
		return common.Err("CONFLICT", "App is stopping")
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return err
	}
	if snapshot.LastSuccess == nil || snapshot.Kind != common.WorkbenchObsidianTasks {
		paths = nil
	}
	if err := s.workbenchPluginAvailable(snapshot.Kind); err != nil {
		return err
	}
	if snapshot.Paused {
		return common.Err("CONFLICT", "Connection is paused")
	}
	if s.workbenchActive[id] != nil {
		return nil
	}
	if !snapshot.Capabilities.Refresh {
		return common.Err("INVALID_ARGUMENT", "Connector does not support source refresh")
	}
	now := time.Now().UTC()
	for i := range snapshot.Attempts {
		if snapshot.Attempts[i].State == "running" {
			snapshot.Attempts[i].State = "interrupted"
			snapshot.Attempts[i].Finished = &now
		}
	}
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), State: "running", Started: now, Trigger: trigger,
		Scope: "full", ConnectionVersion: snapshot.Version, SnapshotVersion: snapshot.SnapshotVersion}
	if len(paths) > 0 {
		attempt.Scope, attempt.Paths = "notes", append([]string(nil), paths...)
	}
	attempts := append([]common.WorkbenchAttempt{attempt}, snapshot.Attempts...)
	if len(attempts) > 21 {
		attempts = attempts[:21]
	}
	conn := snapshot.WorkbenchConnection
	if err := st.RecordWorkbench(conn, attempts, nil, nil, nil); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, s.workbenchReadTimeout(conn))
	if conn.Kind == common.WorkbenchObsidianTasks && filepath.IsAbs(conn.Vault) {
		ctx = context.WithValue(ctx, workbenchWatchReadyKey{}, s.requestWorkbenchWatchReadyLocked(conn))
	}
	ctx = integrations.WithWorkbenchReadProgress(ctx, func(progress common.WorkbenchReadProgress) {
		s.readProgress.Store(attempt.ID, progress)
	})
	s.workbenchActive[id] = cancel
	s.wg.Add(1)
	go s.runWorkbench(ctx, cancel, st, conn, attempts, snapshot.SnapshotVersion, paths, s.workbenchReader)
	return nil
}

func (s *Service) runWorkbench(ctx context.Context, cancel context.CancelFunc, st workbenchStore, c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, version int64, paths []string, read workbenchReader) {
	defer s.wg.Done()
	defer s.readProgress.Delete(attempts[0].ID)
	defer cancel()
	if ready, ok := ctx.Value(workbenchWatchReadyKey{}).(<-chan struct{}); ok {
		select {
		case <-ready:
		case <-ctx.Done():
		}
	}
	s.mu.Lock()
	watch, watchOK := s.workbenchWatchReady[c.ID]
	watchOK = watchOK && watch.version == c.Version && watch.installed
	watchRevision := watch.revision
	s.mu.Unlock()
	before, err := st.WorkbenchObjects(c.ID)
	result := common.WorkbenchReadResult{ConnectionID: c.ID, ConnectionVersion: c.Version, SnapshotVersion: version,
		Scope: common.WorkbenchReadFull, Coverage: common.WorkbenchReadUnverified, Err: err}
	if err == nil {
		_, allowNotes := st.(interface {
			CommitWorkbenchObservedTasks(common.WorkbenchConnection, int64, []common.WorkbenchTask) error
		})
		result = observeWorkbenchConnection(ctx, c, version, before, paths, allowNotes, read)
	}
	err = result.Err
	partial := result.Scope == common.WorkbenchReadNotes
	tasks, updates := result.Tasks, result.Tasks
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workbenchActive, c.ID)
	defer func() {
		if s.workbenchDirty[c.ID] {
			delete(s.workbenchDirty, c.ID)
			delete(s.workbenchPaths, c.ID)
			if !s.stopping {
				_ = s.startWorkbenchLocked(st, c.ID, "source_changed")
			}
		} else if pending := s.workbenchPaths[c.ID]; len(pending) > 0 {
			delete(s.workbenchPaths, c.ID)
			paths := make([]string, 0, len(pending))
			for path := range pending {
				paths = append(paths, path)
			}
			if !s.stopping {
				_ = s.startWorkbenchPathsLocked(st, c.ID, "source_changed", paths)
			}
		}
	}()
	current, currentErr := s.WorkbenchSnapshot(c.ID)
	now := time.Now().UTC()
	attempt := &attempts[0]
	attempt.Finished = &now
	attempt.Scope, attempt.Paths, attempt.Coverage = result.Scope, result.Paths, result.Coverage
	if currentErr != nil {
		return
	}
	if len(current.Attempts) == 0 || current.Attempts[0].ID != attempt.ID {
		if current.SnapshotVersion != version && !s.stopping && !current.Paused {
			_ = s.startWorkbenchLocked(st, c.ID, "source_changed")
		}
		return
	}
	if current.Version != c.Version {
		attempt.State = "superseded"
		attempt.Coverage = "unverified"
		attempt.Paths = nil
		attempts = attempts[:min(20, len(attempts))]
		_ = finishWorkbenchAttempt(st, current.WorkbenchConnection, *attempt, attempts)
		if !s.stopping && !current.Paused {
			_ = s.startWorkbenchLocked(st, c.ID, "configuration")
		}
		return
	}
	attempts = attempts[:min(20, len(attempts))]
	if errors.Is(ctx.Err(), context.Canceled) || current.PluginDisabled {
		attempt.State = "cancelled"
		attempt.Coverage = "unverified"
		_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		attempt.State = "failed"
		attempt.Coverage = "unverified"
		attempt.Error = "Source read timed out after " + s.workbenchReadTimeout(c).String()
		_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
		return
	}
	if err != nil {
		attempt.State = "failed"
		attempt.Coverage = "unverified"
		attempt.Error = "Source could not be read completely"
		if c.Kind == common.WorkbenchObsidianTasks {
			if problem, ok := errors.AsType[*common.Error](err); ok {
				attempt.Error = problem.Message
			}
		}
		_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
		if result.Retry {
			s.workbenchDirty[c.ID] = true
		}
		return
	}
	// A source hint observed during this read makes its coverage stale. The
	// deferred scheduler keeps the hint and reads again from the current source.
	currentWatch, hasCurrentWatch := s.workbenchWatchReady[c.ID]
	watchChanged := watchOK && (!hasCurrentWatch || currentWatch.version != c.Version || !currentWatch.installed || currentWatch.revision != watchRevision)
	if s.workbenchDirty[c.ID] || len(s.workbenchPaths[c.ID]) > 0 || watchChanged {
		attempt.State = "superseded"
		attempt.Coverage = "unverified"
		if watchChanged {
			if hasCurrentWatch && currentWatch.version == c.Version && currentWatch.installed && currentWatch.verified && len(currentWatch.changed) > 0 {
				if s.workbenchPaths[c.ID] == nil {
					s.workbenchPaths[c.ID] = make(map[string]bool)
				}
				for path := range currentWatch.changed {
					s.workbenchPaths[c.ID][path] = true
				}
			} else {
				s.workbenchDirty[c.ID] = true
			}
		}
		_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
		return
	}
	if partial {
		attempt.State = "success"
		attempt.Coverage = "complete"
		updatedIDs := make(map[string]bool, len(updates))
		for _, task := range updates {
			updatedIDs[task.ID] = true
		}
		var previous []common.WorkbenchTask
		for _, task := range before {
			if updatedIDs[task.ID] {
				previous = append(previous, task)
			}
		}
		countWorkbenchChanges(attempt, previous, updates)
		store := st.(interface {
			CommitWorkbenchObservedTasks(common.WorkbenchConnection, int64, []common.WorkbenchTask) error
		})
		if err := store.CommitWorkbenchObservedTasks(c, version, updates); err != nil {
			attempt.State = "failed"
			attempt.Coverage = "unverified"
			attempt.Error = "Snapshot could not be saved"
			if problem, ok := errors.AsType[*common.Error](err); ok && problem.Code == "STALE_REVISION" {
				attempt.State = "superseded"
				attempt.Error = ""
				s.workbenchDirty[c.ID] = true
			}
			_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
			return
		}
		_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
		return
	}
	var ambiguous map[string]bool
	if c.Kind == common.WorkbenchObsidianTasks {
		ambiguous = ambiguousWorkbenchNativeIdentities(before, tasks)
	}
	matchBefore, tasks, preserved := partitionWorkbenchAmbiguous(before, tasks, ambiguous)
	matchWorkbench(c, matchBefore, tasks)
	recordObsidianImport(c, matchBefore, tasks, now)
	tasks = append(tasks, preserved...)
	countWorkbenchChanges(attempt, before, tasks)
	attempt.State = "success"
	attempt.Coverage = "complete"
	if len(ambiguous) > 0 {
		attempt.State = "failed"
		attempt.Coverage = "partial"
		attempt.Error = "Source contains duplicate task IDs; previous values were retained"
	}
	var publishErr error
	if len(ambiguous) > 0 {
		if partialStore, ok := st.(interface {
			RecordWorkbenchPartialSnapshot(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, time.Time, int64) error
		}); ok {
			publishErr = partialStore.RecordWorkbenchPartialSnapshot(c, attempts, before, tasks, now, version)
		} else {
			_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
			return
		}
	} else if guarded, ok := st.(interface {
		RecordWorkbenchIfVersion(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, *time.Time, int64) error
	}); ok {
		publishErr = guarded.RecordWorkbenchIfVersion(c, attempts, before, tasks, &now, version)
	} else {
		publishErr = st.RecordWorkbench(c, attempts, before, tasks, &now)
	}
	if problem, ok := errors.AsType[*common.Error](publishErr); ok && problem.Code == "STALE_REVISION" {
		attempt.State = "superseded"
		attempt.Coverage = "unverified"
		_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
		if !s.stopping && !current.Paused {
			_ = s.startWorkbenchLocked(st, c.ID, "source_changed")
		}
		return
	}
	if publishErr != nil {
		attempt.State = "failed"
		attempt.Coverage = "unverified"
		attempt.Error = "Snapshot could not be saved"
		_ = finishWorkbenchAttempt(st, c, *attempt, attempts)
		return
	}
	if c.Kind == common.WorkbenchObsidianTasks && c.ReadMethod != common.WorkbenchObsidianCLI && len(ambiguous) == 0 && watchOK {
		currentWatch, ok := s.workbenchWatchReady[c.ID]
		if ok && currentWatch.version == c.Version && currentWatch.installed && currentWatch.revision == watchRevision {
			currentWatch.verified = true
			currentWatch.changed = nil
			s.workbenchWatchReady[c.ID] = currentWatch
		}
	}
}

func finishWorkbenchAttempt(st workbenchStore, c common.WorkbenchConnection, attempt common.WorkbenchAttempt, fallback []common.WorkbenchAttempt) error {
	if store, ok := st.(interface {
		FinishWorkbenchAttempt(common.WorkbenchConnection, common.WorkbenchAttempt) error
	}); ok {
		return store.FinishWorkbenchAttempt(c, attempt)
	}
	return st.RecordWorkbench(c, fallback, nil, nil, nil)
}

// enqueueWorkbenchSourceChange coalesces hints while a source read is active.
func (s *Service) enqueueWorkbenchSourceChange(st workbenchStore, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	if s.workbenchActive[id] != nil {
		s.workbenchDirty[id] = true
		delete(s.workbenchPaths, id)
		return
	}
	_ = s.startWorkbenchLocked(st, id, "source_changed")
}

// Path hints are bounded; loss of precision becomes a full verification.
func (s *Service) enqueueWorkbenchPathChange(st workbenchStore, id, path string) {
	if path == "" {
		s.enqueueWorkbenchSourceChange(st, id)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	if s.workbenchActive[id] == nil {
		_ = s.startWorkbenchPathsLocked(st, id, "source_changed", []string{path})
		return
	}
	if s.workbenchDirty[id] {
		return
	}
	if s.workbenchPaths[id] == nil {
		s.workbenchPaths[id] = make(map[string]bool)
	}
	s.workbenchPaths[id][path] = true
	if len(s.workbenchPaths[id]) > 64 {
		delete(s.workbenchPaths, id)
		s.workbenchDirty[id] = true
	}
}

func (s *Service) startScheduledWorkbench(st workbenchStore, trigger string) {
	s.startScheduledWorkbenchMatching(st, trigger, func(string) bool { return true })
}

func (s *Service) startScheduledWorkbenchMatching(st workbenchStore, trigger string, matches func(string) bool) {
	connections, err := st.WorkbenchConnections()
	if err != nil {
		return
	}
	for _, c := range connections {
		if !matches(c.Kind) || !s.workbenchPlugins[c.Kind].capabilities.Refresh {
			continue
		}
		s.mu.Lock()
		_ = s.startWorkbenchLocked(st, c.ID, trigger)
		s.mu.Unlock()
	}
}

func (s *Service) tickWorkbench(st workbenchStore) {
	defer s.wg.Done()
	interval := s.obsidianSync.ScanInterval
	if interval <= 0 {
		interval = time.Minute
	}
	obsidianTimer := time.NewTicker(interval)
	defer obsidianTimer.Stop()
	otherTimer := time.NewTicker(time.Minute)
	defer otherTimer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-obsidianTimer.C:
			s.startScheduledWorkbenchMatching(st, "periodic", func(kind string) bool {
				return kind == common.WorkbenchObsidianTasks
			})
		case <-otherTimer.C:
			s.startScheduledWorkbenchMatching(st, "periodic", func(kind string) bool {
				return kind != common.WorkbenchObsidianTasks
			})
		}
	}
}

func countWorkbenchChanges(attempt *common.WorkbenchAttempt, before, tasks []common.WorkbenchTask) {
	old := map[string]common.WorkbenchTask{}
	for _, task := range before {
		old[task.ID] = task
	}
	for _, task := range tasks {
		prior, ok := old[task.ID]
		if !ok {
			attempt.Added++
		} else if !reflect.DeepEqual(prior, task) {
			attempt.Updated++
		}
		delete(old, task.ID)
	}
	attempt.Removed = len(old)
}
