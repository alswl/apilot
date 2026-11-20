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
		task.TargetVersion = workbenchFingerprint(*task)
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
	task.TargetVersion = workbenchFingerprint(task)
	task.ConnectionVersion = connection.Version
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
	bridge, err := s.prepareWorkbenchBridge(&c, prior, input)
	if err != nil {
		return "", err
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

func workbenchReadTimeout(c common.WorkbenchConnection) time.Duration {
	if c.ReadMethod == common.WorkbenchObsidianCLI {
		return 90 * time.Second
	}
	return 30 * time.Second
}

func (s *Service) startWorkbenchLocked(st workbenchStore, id, trigger string) error {
	if s.stopping {
		return common.Err("CONFLICT", "App is stopping")
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return err
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
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), State: "running", Started: now, Trigger: trigger}
	attempts := append([]common.WorkbenchAttempt{attempt}, snapshot.Attempts...)
	if len(attempts) > 21 {
		attempts = attempts[:21]
	}
	conn := snapshot.WorkbenchConnection
	if err := st.RecordWorkbench(conn, attempts, nil, nil, nil); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(s.ctx, workbenchReadTimeout(conn))
	ctx = integrations.WithWorkbenchReadProgress(ctx, func(progress common.WorkbenchReadProgress) {
		s.readProgress.Store(attempt.ID, progress)
	})
	s.workbenchActive[id] = cancel
	s.wg.Add(1)
	go s.runWorkbench(ctx, cancel, st, conn, attempts, s.workbenchReader)
	return nil
}

func (s *Service) runWorkbench(ctx context.Context, cancel context.CancelFunc, st workbenchStore, c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, read workbenchReader) {
	defer s.wg.Done()
	defer s.readProgress.Delete(attempts[0].ID)
	defer cancel()
	before, err := st.WorkbenchObjects(c.ID)
	var tasks []common.WorkbenchTask
	if err == nil {
		tasks, err = read(ctx, c)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.workbenchActive, c.ID)
	current, currentErr := s.WorkbenchSnapshot(c.ID)
	now := time.Now().UTC()
	attempt := &attempts[0]
	attempt.Finished = &now
	if currentErr != nil || len(current.Attempts) == 0 || current.Attempts[0].ID != attempt.ID {
		return
	}
	if current.Version != c.Version {
		attempt.State = "superseded"
		attempts = attempts[:min(20, len(attempts))]
		_ = st.RecordWorkbench(current.WorkbenchConnection, attempts, nil, nil, nil)
		if !s.stopping && !current.Paused {
			_ = s.startWorkbenchLocked(st, c.ID, "configuration")
		}
		return
	}
	attempts = attempts[:min(20, len(attempts))]
	if errors.Is(ctx.Err(), context.Canceled) || current.PluginDisabled {
		attempt.State = "cancelled"
		_ = st.RecordWorkbench(c, attempts, nil, nil, nil)
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		attempt.State = "failed"
		attempt.Error = "Source read timed out after " + workbenchReadTimeout(c).String()
		_ = st.RecordWorkbench(c, attempts, nil, nil, nil)
		return
	}
	if err != nil {
		attempt.State = "failed"
		attempt.Error = "Source could not be read completely"
		if c.Kind == common.WorkbenchObsidianTasks {
			if problem, ok := errors.AsType[*common.Error](err); ok {
				attempt.Error = problem.Message
			}
		}
		_ = st.RecordWorkbench(c, attempts, nil, nil, nil)
		return
	}
	matchWorkbench(c, before, tasks)
	recordObsidianImport(c, before, tasks, now)
	countWorkbenchChanges(attempt, before, tasks)
	attempt.State = "success"
	if err := st.RecordWorkbench(c, attempts, before, tasks, &now); err != nil {
		attempt.State = "failed"
		attempt.Error = "Snapshot could not be saved"
		_ = st.RecordWorkbench(c, attempts, nil, nil, nil)
	}
}

func (s *Service) startScheduledWorkbench(st workbenchStore, trigger string) {
	connections, err := st.WorkbenchConnections()
	if err != nil {
		return
	}
	for _, c := range connections {
		if !s.workbenchPlugins[c.Kind].capabilities.Refresh {
			continue
		}
		s.mu.Lock()
		_ = s.startWorkbenchLocked(st, c.ID, trigger)
		s.mu.Unlock()
	}
}

func (s *Service) tickWorkbench(st workbenchStore) {
	defer s.wg.Done()
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			s.startScheduledWorkbench(st, "periodic")
			timer.Reset(time.Minute)
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
