package services

import (
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func (s *Service) RemoveWorkbench(id string) error {
	s.githubCredentialMu.Lock()
	defer s.githubCredentialMu.Unlock()
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return common.Err("CONFLICT", "App is stopping")
	}
	st, err := s.workbenchStore()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	if id == common.DefaultWorkbenchLocalID {
		s.mu.Unlock()
		return common.Err("CONFLICT", "The built-in Local connection can be cleared by importing an empty tasks array")
	}
	ref := ""
	if store, ok := s.store.(workbenchGitHubStore); ok {
		if progress, err := store.WorkbenchGitHubProgress(id); err == nil {
			ref = progress.CredentialRef
		}
	}
	if err := st.DeleteWorkbench(id); err != nil {
		s.mu.Unlock()
		return err
	}
	if cancel := s.workbenchActive[id]; cancel != nil {
		cancel()
	}
	s.mu.Unlock()
	if ref != "" {
		if credentials, err := s.workbenchGitHubCredentials(); err == nil {
			s.deleteUnusedGitHubCredential(credentials, ref)
		}
	}
	return nil
}

func (s *Service) ImportWorkbench(batch common.WorkbenchImport) (common.WorkbenchSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	empty := common.WorkbenchSnapshot{}
	if s.stopping {
		return empty, common.Err("CONFLICT", "App is stopping")
	}
	if err := s.workbenchPluginAvailable(common.WorkbenchLocal); err != nil {
		return empty, err
	}
	if !s.workbenchPlugins[common.WorkbenchLocal].capabilities.Import {
		return empty, common.Err("CONFLICT", "Connector does not support imports")
	}
	batch.Name = strings.TrimSpace(batch.Name)
	if len(batch.Name) > 256 || batch.Tasks == nil {
		return empty, common.Err("INVALID_ARGUMENT", "Provide a tasks array and an optional connection name of at most 256 bytes")
	}
	tasks := make([]common.WorkbenchTask, 0, len(batch.Tasks))
	keys := map[string]bool{}
	for _, input := range batch.Tasks {
		if input.Key == "" || len(input.Key) > 256 || strings.ContainsRune(input.Key, 0) || keys[input.Key] ||
			strings.TrimSpace(input.Title) == "" || len(input.Title) > 1024 || len(input.Summary) > 8<<10 || !validWorkbenchState(input.State) {
			return empty, common.Err("INVALID_ARGUMENT", "Each task needs a unique key, a title and a normalized state")
		}
		keys[input.Key] = true
		tasks = append(tasks, common.WorkbenchTask{SourceKey: input.Key, NativeID: input.Key, Title: input.Title, State: input.State, Summary: input.Summary, Locator: "import:" + input.Key, NativeFields: map[string]string{"status": input.State}})
	}
	st, err := s.workbenchStore()
	if err != nil {
		return empty, err
	}
	list, err := st.WorkbenchConnections()
	if err != nil {
		return empty, err
	}
	c := common.WorkbenchConnection{ID: common.ID(), Version: 1, Kind: common.WorkbenchLocal, Mode: "local", Name: batch.Name}
	var attempts []common.WorkbenchAttempt
	existing := false
	for _, snapshot := range list {
		matches := strings.EqualFold(snapshot.Name, batch.Name)
		if batch.Name == "" {
			matches = snapshot.ID == common.DefaultWorkbenchLocalID
		}
		if !matches {
			continue
		}
		if snapshot.Kind != common.WorkbenchLocal {
			return empty, common.Err("CONFLICT", "Connection name belongs to another source")
		}
		c, attempts = snapshot.WorkbenchConnection, snapshot.Attempts
		existing = true
		break
	}
	if batch.Name == "" && !existing {
		return empty, common.Err("INTERNAL", "Built-in Local connection unavailable")
	}
	var before []common.WorkbenchTask
	if existing {
		before, err = st.WorkbenchObjects(c.ID)
		if err != nil {
			return empty, err
		}
	}
	matchWorkbench(c, before, tasks)
	now := time.Now().UTC()
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), Trigger: "import", State: "success", Started: now, Finished: &now}
	countWorkbenchChanges(&attempt, before, tasks)
	attempts = append([]common.WorkbenchAttempt{attempt}, attempts...)
	if err := st.CommitWorkbenchImport(c, attempts[:min(20, len(attempts))], before, tasks, &now); err != nil {
		return empty, err
	}
	return s.WorkbenchSnapshot(c.ID)
}
