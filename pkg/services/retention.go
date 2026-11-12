package services

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Service) Prune() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return common.Err("CONFLICT", "App is stopping")
	}
	cutoff := time.Now().Add(-time.Duration(s.settings.RetentionDays) * 24 * time.Hour)
	for id, r := range s.runs {
		if !r.Active() && r.CreatedAt.Before(cutoff) {
			expected := filepath.Join(s.dir, "logs", r.ID)
			if r.LogPath != expected {
				return fmt.Errorf("refusing unsafe log path")
			}
			if e := os.RemoveAll(expected); e != nil {
				return e
			}
			if e := s.store.DeleteRun(id); e != nil {
				return e
			}
			delete(s.runs, id)
		}
	}
	events, e := s.store.Events()
	if e != nil {
		return e
	}
	for _, v := range events {
		if v.CreatedAt.Before(cutoff) {
			if e = s.store.DeleteEvent(v.ID); e != nil {
				return e
			}
		}
	}
	return nil
}
