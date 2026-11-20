package services

import "github.com/alswl/apilot/core/pkg/common"

func (s *Service) PauseWorkbench(id string, version int64, paused bool) (common.WorkbenchSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stopping {
		return common.WorkbenchSnapshot{}, common.Err("CONFLICT", "App is stopping")
	}
	store, err := s.workbenchStore()
	if err != nil {
		return common.WorkbenchSnapshot{}, err
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return snapshot, err
	}
	if !s.workbenchPlugins[snapshot.Kind].capabilities.Refresh {
		return snapshot, common.Err("INVALID_ARGUMENT", "Local does not run scheduled scans")
	}
	if snapshot.Version != version {
		return snapshot, common.Err("STALE_REVISION", "Connection configuration changed")
	}
	if !paused && snapshot.PluginDisabled {
		return snapshot, common.Err("CONFLICT", "Connector plugin is disabled")
	}
	if snapshot.Paused == paused {
		return snapshot, nil
	}
	connection := snapshot.WorkbenchConnection
	connection.Paused = paused
	connection.Version++
	if err := store.SaveWorkbench(connection, false); err != nil {
		return snapshot, err
	}
	if cancel := s.workbenchActive[id]; cancel != nil {
		cancel()
	} else if !paused {
		if err := s.startWorkbenchLocked(store, id, "resume"); err != nil {
			return snapshot, err
		}
	}
	return s.WorkbenchSnapshot(id)
}

func (s *Service) CancelWorkbenchSync(id string, attemptID int64) (common.WorkbenchSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return snapshot, err
	}
	for i, attempt := range snapshot.Attempts {
		if attempt.ID != attemptID {
			continue
		}
		if attempt.State == "cancelled" {
			return snapshot, nil
		}
		if i == 0 && attempt.State == "running" {
			if cancel := s.workbenchActive[id]; cancel != nil {
				cancel()
				return snapshot, nil
			}
		}
		break
	}
	return snapshot, common.Err("CONFLICT", "Sync attempt is no longer active")
}

func (s *Service) WorkbenchSyncDetails(id string) (common.WorkbenchSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return snapshot, err
	}
	if store, ok := s.store.(interface {
		WorkbenchPendingWrites(string) ([]common.WorkbenchTaskReceipt, error)
	}); ok {
		snapshot.PendingWrites, err = store.WorkbenchPendingWrites(id)
		for i := range snapshot.PendingWrites {
			receipt := &snapshot.PendingWrites[i]
			if receipt.SyncState == common.WorkbenchOperationNeedsReview && s.operationRunning[receipt.RequestID] {
				receipt.SyncState = common.WorkbenchOperationDispatching
			}
		}
	}
	return snapshot, err
}
