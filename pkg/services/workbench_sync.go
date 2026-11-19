package services

import (
	"context"
	"maps"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/integrations"
)

type workbenchReader func(context.Context, common.WorkbenchConnection) ([]common.WorkbenchTask, error)

func readWorkbench(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
	if c.Mode == "mock" {
		return integrations.ReadWorkbenchGitHubMock(c)
	}
	return integrations.ReadWorkbenchObsidian(ctx, c)
}

type workbenchStore interface {
	WorkbenchConnections() ([]common.WorkbenchSnapshot, error)
	SaveWorkbench(common.WorkbenchConnection, bool) error
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
	return st.WorkbenchConnections()
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
	return st.WorkbenchPage(q)
}

func (s *Service) WorkbenchTask(id string) (common.WorkbenchTask, error) {
	st, err := s.workbenchStore()
	if err != nil {
		return common.WorkbenchTask{}, err
	}
	return st.WorkbenchObject(id)
}

func (s *Service) SaveWorkbench(c common.WorkbenchConnection) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return "", common.Err("CONFLICT", "App is stopping")
	}
	st, err := s.workbenchStore()
	if err != nil {
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
		reset = c.Vault != old.Vault || c.Scope != old.Scope || c.Repository != old.Repository || !maps.Equal(c.Statuses, old.Statuses)
		if reset {
			c.Version++
			trigger = "configuration"
		}
	}
	if err := st.SaveWorkbench(c, reset); err != nil {
		return "", err
	}
	if reset && s.workbenchActive[c.ID] != nil {
		s.workbenchActive[c.ID]()
	}
	return c.ID, s.startWorkbenchLocked(st, c.ID, trigger)
}

func (s *Service) SyncWorkbench(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.workbenchStore()
	if err != nil {
		return err
	}
	return s.startWorkbenchLocked(st, id, "manual")
}

func (s *Service) startWorkbenchLocked(st workbenchStore, id, trigger string) error {
	if s.stopping {
		return common.Err("CONFLICT", "App is stopping")
	}
	if s.workbenchActive[id] != nil {
		return nil
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return err
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
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	s.workbenchActive[id] = cancel
	s.wg.Add(1)
	go s.runWorkbench(ctx, cancel, st, conn, attempts, s.workbenchReader)
	return nil
}

func (s *Service) runWorkbench(ctx context.Context, cancel context.CancelFunc, st workbenchStore, c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, read workbenchReader) {
	defer s.wg.Done()
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
	if currentErr != nil {
		return
	}
	if current.Version != c.Version {
		attempt.State = "superseded"
		attempts = attempts[:min(20, len(attempts))]
		_ = st.RecordWorkbench(current.WorkbenchConnection, attempts, nil, nil, nil)
		if !s.stopping {
			_ = s.startWorkbenchLocked(st, c.ID, "configuration")
		}
		return
	}
	attempts = attempts[:min(20, len(attempts))]
	if ctx.Err() != nil {
		attempt.State = "cancelled"
		_ = st.RecordWorkbench(c, attempts, nil, nil, nil)
		return
	}
	if err != nil {
		attempt.State = "failed"
		attempt.Error = "Source could not be read completely"
		_ = st.RecordWorkbench(c, attempts, nil, nil, nil)
		return
	}
	matchWorkbench(c, before, tasks)
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
	attempt.State = "success"
	if err := st.RecordWorkbench(c, attempts, before, tasks, &now); err != nil {
		attempt.State = "failed"
		attempt.Error = "Snapshot could not be saved"
		_ = st.RecordWorkbench(c, attempts, nil, nil, nil)
	}
}

func (s *Service) tickWorkbench(st workbenchStore) {
	defer s.wg.Done()
	timer := time.NewTimer(0)
	defer timer.Stop()
	trigger := "startup"
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			connections, err := st.WorkbenchConnections()
			if err == nil {
				for _, c := range connections {
					s.mu.Lock()
					_ = s.startWorkbenchLocked(st, c.ID, trigger)
					s.mu.Unlock()
				}
			}
			trigger = "periodic"
			timer.Reset(time.Minute)
		}
	}
}
