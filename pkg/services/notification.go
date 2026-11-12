package services

import (
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Service) AllowNotification(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return false, common.Err("CONFLICT", "App is stopping")
	}
	return s.store.AllowNotification(key, time.Now())
}
