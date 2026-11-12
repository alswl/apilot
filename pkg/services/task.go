package services

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/managers"
)

func (s *Service) taskLocked(ref string) (common.Task, error) {
	if t, ok := s.tasks[ref]; ok {
		return t, nil
	}
	for _, t := range s.tasks {
		if t.Name == ref {
			return t, nil
		}
	}
	return common.Task{}, common.Err("NOT_FOUND", "Task not found")
}

func public(t common.Task) common.Task {
	t.NextRunAt = nil
	if t.Schedule != nil {
		t.NextRunAt = t.Schedule.NextRunAt
	}
	t.EnvKeys = []string{}
	for k := range t.Env {
		t.EnvKeys = append(t.EnvKeys, k)
	}
	sort.Strings(t.EnvKeys)
	t.Env = nil
	if t.Args == nil {
		t.Args = []string{}
	}
	if t.Schedule != nil {
		cp := *t.Schedule
		t.Schedule = &cp
	}
	return t
}

func (s *Service) Tasks() []common.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []common.Task{}
	for _, t := range s.tasks {
		t = public(t)
		for _, r := range s.runs {
			if r.TaskID == t.ID && (t.LastRun == nil || r.CreatedAt.After(t.LastRun.CreatedAt)) {
				cp := r
				t.LastRun = &cp
			}
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Service) Task(ref string) (common.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, e := s.taskLocked(ref)
	if e == nil {
		for _, r := range s.runs {
			if r.TaskID == t.ID && (t.LastRun == nil || r.CreatedAt.After(t.LastRun.CreatedAt)) {
				cp := r
				t.LastRun = &cp
			}
		}
	}
	return public(t), e
}

func (s *Service) SaveTask(ref string, in common.TaskInput, expected int64) (common.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return common.Task{}, common.Err("CONFLICT", "App is stopping")
	}
	now := time.Now().UTC()
	t := common.Task{ID: common.ID(), CreatedAt: now, Env: map[string]string{}}
	if ref != "" {
		var e error
		t, e = s.taskLocked(ref)
		if e != nil {
			return t, e
		}
		if expected != 0 && t.Revision != expected {
			return t, common.Err("STALE_REVISION", "Task changed; reload before saving")
		}
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 128 || strings.ContainsAny(in.Name, "\r\n\t") {
		return t, common.Err("INVALID_ARGUMENT", "Task name must contain 1–128 characters without control characters")
	}
	if in.Executable == "" || !filepath.IsAbs(in.Cwd) || in.TimeoutMS <= 0 {
		return t, common.Err("INVALID_ARGUMENT", "Executable, absolute working directory and positive timeout are required")
	}
	for _, x := range s.tasks {
		if x.ID != t.ID && x.Name == in.Name {
			return t, common.Err("CONFLICT", "A task with that name already exists")
		}
	}
	t.Name = in.Name
	t.Description = in.Description
	t.Executable = in.Executable
	t.Args = append([]string{}, in.Args...)
	t.Cwd = in.Cwd
	t.Enabled = in.Enabled
	t.TimeoutMS = in.TimeoutMS
	t.Revision++
	t.UpdatedAt = now
	env := map[string]string{}
	for k, v := range t.Env {
		env[k] = v
	}
	for k, v := range in.EnvSet {
		if strings.ContainsAny(k, "=\x00") || k == "" || strings.ContainsRune(v, 0) {
			return t, common.Err("INVALID_ARGUMENT", "Invalid environment variable")
		}
		env[k] = v
	}
	for _, k := range in.EnvUnset {
		delete(env, k)
	}
	t.Env = env
	t.Schedule = nil
	if in.Schedule != nil {
		cp := *in.Schedule
		if cp.Timezone == "" || cp.Timezone == "Local" {
			zone, e := managers.SystemTimezone()
			if e != nil {
				return t, common.Err("INVALID_ARGUMENT", e.Error())
			}
			cp.Timezone = zone
		}
		if cp.ID == "" {
			cp.ID = common.ID()
		}
		t.Schedule = &cp
	}
	if e := s.resetSchedule(&t, now); e != nil {
		return t, common.Err("INVALID_ARGUMENT", e.Error())
	}
	if e := s.store.SaveTask(t); e != nil {
		return t, e
	}
	s.tasks[t.ID] = t
	return public(t), nil
}

func (s *Service) SetEnabled(ref string, on bool) (common.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, e := s.taskLocked(ref)
	if e != nil {
		return t, e
	}
	if s.stopping {
		return t, common.Err("CONFLICT", "App is stopping")
	}
	t.Enabled = on
	t.Revision++
	t.UpdatedAt = time.Now().UTC()
	if e = s.resetSchedule(&t, t.UpdatedAt); e != nil {
		return t, e
	}
	if e = s.store.SaveTask(t); e != nil {
		return t, e
	}
	s.tasks[t.ID] = t
	return public(t), nil
}

func (s *Service) DeleteTask(ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return common.Err("CONFLICT", "App is stopping")
	}
	t, e := s.taskLocked(ref)
	if e != nil {
		return e
	}
	for id := range s.running {
		if s.runs[id].TaskID == t.ID {
			return common.Err("CONFLICT", "Stop the active run before deleting this task")
		}
	}
	if e = s.store.DeleteTask(t.ID); e != nil {
		return e
	}
	delete(s.tasks, t.ID)
	return nil
}

func (s *Service) SetAutomatic(ref string, on bool, expectedRevision int64) (common.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return common.Task{}, common.Err("CONFLICT", "App is stopping")
	}
	t, e := s.taskLocked(ref)
	if e != nil {
		return t, e
	}
	if expectedRevision != 0 && expectedRevision != t.Revision {
		return common.Task{}, common.Err("STALE_REVISION", "Task changed; reload before saving")
	}
	if t.Schedule == nil {
		return public(t), common.Err("INVALID_ARGUMENT", "Task has no schedule")
	}
	cp := *t.Schedule
	t.Schedule = &cp
	t.Schedule.Enabled = on
	t.Enabled = on
	t.Revision++
	t.UpdatedAt = time.Now().UTC()
	if e = s.resetSchedule(&t, t.UpdatedAt); e != nil {
		return common.Task{}, e
	}
	if e = s.store.SaveTask(t); e != nil {
		return common.Task{}, e
	}
	s.tasks[t.ID] = t
	return public(t), nil
}
