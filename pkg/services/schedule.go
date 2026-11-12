package services

import (
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/managers"
)

func (s *Service) resetSchedule(t *common.Task, now time.Time) error {
	if t.Schedule == nil {
		return nil
	}
	cp := *t.Schedule
	t.Schedule = &cp
	next, e := managers.Next(t.Schedule, now)
	if e != nil {
		return e
	}
	t.Schedule.NextRunAt = nil
	if t.Enabled && t.Schedule.Enabled {
		t.Schedule.NextRunAt = &next
	}
	return nil
}

func (s *Service) Resume() {
	s.mu.Lock()
	s.suspended = false
	for id, v := range s.observers {
		s.observerNext[id] = time.Now().Add(time.Duration(v.IntervalMS) * time.Millisecond)
	}
	defer s.mu.Unlock()
	for id, t := range s.tasks {
		_ = s.resetSchedule(&t, time.Now().UTC())
		s.tasks[id] = t
	}
}

func (s *Service) tick() {
	defer s.wg.Done()
	timer := time.NewTicker(250 * time.Millisecond)
	defer timer.Stop()
	last := time.Now()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			now := time.Now()
			if now.UnixNano()-last.UnixNano() > int64(2*time.Second) {
				s.Resume()
				last = now
				continue
			}
			last = now
			s.mu.Lock()
			if s.suspended {
				s.mu.Unlock()
				continue
			}
			for id, t := range s.tasks {
				if t.Schedule == nil || t.Schedule.NextRunAt == nil {
					continue
				}
				due := *t.Schedule.NextRunAt
				if due.After(now) {
					continue
				}
				_ = s.resetSchedule(&t, now)
				s.tasks[id] = t
				if now.Sub(due) <= time.Second {
					_, e := s.runLocked(t, "scheduled", &due)
					if e != nil {
						s.lastError = e.Error()
					}
				}
			}
			s.pollObservers(now)
			s.mu.Unlock()
		}
	}
}
