package services

import (
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Service) Events() ([]common.AttentionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, common.Err("CONFLICT", "App is stopping")
	}
	return s.store.Events()
}

func (s *Service) AckEvent(id string) (common.AttentionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return common.AttentionEvent{}, common.Err("CONFLICT", "App is stopping")
	}
	events, e := s.store.Events()
	if e != nil {
		return common.AttentionEvent{}, e
	}
	for _, v := range events {
		if v.ID == id {
			if v.AcknowledgedAt == nil {
				now := time.Now().UTC()
				v.AcknowledgedAt = &now
				e = s.store.SaveEvent(v)
			}
			return v, e
		}
	}
	return common.AttentionEvent{}, common.Err("NOT_FOUND", "Event not found")
}
