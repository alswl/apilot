package services

import (
	"github.com/alswl/apilot/pkg/common"
)

func (s *Service) Settings() common.Settings { s.mu.Lock(); defer s.mu.Unlock(); return s.settings }

func (s *Service) SaveSettings(v common.Settings) (common.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return v, common.Err("CONFLICT", "App is stopping")
	}
	if v.MaxConcurrent < len(s.running) {
		return v, common.Err("CONFLICT", "Stop active runs before lowering the limit below the active count")
	}
	if v.DefaultTimeoutMS <= 0 || v.MaxConcurrent < 1 || v.RetentionDays < 1 || v.LogLimitBytes < 1 {
		return v, common.Err("INVALID_ARGUMENT", "Settings values must be positive")
	}
	if v.Revision != s.settings.Revision {
		return v, common.Err("STALE_REVISION", "Settings changed")
	}
	v.Revision++
	if e := s.store.SaveSettings(v); e != nil {
		return v, e
	}
	s.settings = v
	return v, nil
}
