package services

import (
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchSyncRunStore interface {
	WorkbenchSyncRun(string) (common.WorkbenchSyncRun, error)
	WorkbenchSyncRuns(common.WorkbenchSyncQuery) ([]common.WorkbenchSyncRun, error)
	WorkbenchSyncUsage(common.WorkbenchSyncQuery) ([]common.WorkbenchSyncUsage, error)
}

func validateWorkbenchSyncQuery(q *common.WorkbenchSyncQuery) error {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 100 || q.Offset < 0 || (q.Mode != "" && q.Mode != "full" && q.Mode != "delta") {
		return common.Err("INVALID_ARGUMENT", "Invalid sync run filters")
	}
	var times [2]time.Time
	for i, value := range []string{q.From, q.To} {
		if value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return common.Err("INVALID_ARGUMENT", "Sync time bounds require RFC3339 timestamps")
		}
		times[i] = parsed
	}
	if !times[0].IsZero() && !times[1].IsZero() && !times[0].Before(times[1]) {
		return common.Err("INVALID_ARGUMENT", "Start time must precede end time")
	}
	return nil
}

func (s *Service) WorkbenchSyncRun(id string) (common.WorkbenchSyncRun, error) {
	store, ok := s.store.(workbenchSyncRunStore)
	if !ok {
		return common.WorkbenchSyncRun{}, common.Err("INTERNAL", "Sync run storage unavailable")
	}
	return store.WorkbenchSyncRun(id)
}

func (s *Service) WorkbenchSyncRuns(q common.WorkbenchSyncQuery) ([]common.WorkbenchSyncRun, error) {
	if err := validateWorkbenchSyncQuery(&q); err != nil {
		return nil, err
	}
	store, ok := s.store.(workbenchSyncRunStore)
	if !ok {
		return nil, common.Err("INTERNAL", "Sync run storage unavailable")
	}
	return store.WorkbenchSyncRuns(q)
}

func (s *Service) WorkbenchSyncUsage(q common.WorkbenchSyncQuery) ([]common.WorkbenchSyncUsage, error) {
	if err := validateWorkbenchSyncQuery(&q); err != nil {
		return nil, err
	}
	store, ok := s.store.(workbenchSyncRunStore)
	if !ok {
		return nil, common.Err("INTERNAL", "Sync usage storage unavailable")
	}
	return store.WorkbenchSyncUsage(q)
}
