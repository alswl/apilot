package app

import (
	"time"

	"github.com/alswl/apilot/internal/model"
)

func (s *Service) SetAutomatic(ref string, on bool, expectedRevision int64) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return model.Task{}, model.Err("CONFLICT", "App is stopping")
	}
	t, e := s.taskLocked(ref)
	if e != nil {
		return t, e
	}
	if expectedRevision != 0 && expectedRevision != t.Revision {
		return model.Task{}, model.Err("STALE_REVISION", "Task changed; reload before saving")
	}
	if t.Schedule == nil {
		return public(t), model.Err("INVALID_ARGUMENT", "Task has no schedule")
	}
	cp := *t.Schedule
	t.Schedule = &cp
	t.Schedule.Enabled = on
	t.Enabled = on
	t.Revision++
	t.UpdatedAt = time.Now().UTC()
	if e = s.resetSchedule(&t, t.UpdatedAt); e != nil {
		return model.Task{}, e
	}
	if e = s.store.SaveTask(t); e != nil {
		return model.Task{}, e
	}
	s.tasks[t.ID] = t
	return public(t), nil
}
