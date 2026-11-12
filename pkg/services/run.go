package services

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/integrations"
)

func (s *Service) RunTask(ref, trigger string) (common.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, e := s.taskLocked(ref)
	if e != nil {
		return common.Run{}, e
	}
	if trigger != "manual" && trigger != "cli" {
		return common.Run{}, common.Err("INVALID_ARGUMENT", "Invalid manual trigger source")
	}
	return s.runLocked(t, trigger, nil)
}

func (s *Service) runLocked(t common.Task, trigger string, scheduledAt *time.Time) (common.Run, error) {
	if s.stopping {
		return common.Run{}, common.Err("CONFLICT", "App is stopping")
	}
	now := time.Now().UTC()
	r := common.Run{ScheduledAt: scheduledAt, ID: common.ID(), TaskID: t.ID, TaskName: t.Name, Executable: t.Executable, Args: t.Args, Trigger: trigger, Status: "queued", CreatedAt: now}
	r.LogPath = filepath.Join(s.dir, "logs", r.ID)
	reason := ""
	if len(s.running) >= s.settings.MaxConcurrent {
		reason = "Global concurrency limit reached"
	}
	for id := range s.running {
		if s.runs[id].TaskID == t.ID {
			reason = "Task already has an active run"
		}
	}
	if reason != "" {
		r.Status = "skipped"
		r.ErrorCode = "CONCURRENCY_LIMIT"
		r.Error = reason
		r.FinalizedAt = &now
	}
	if e := s.store.SaveRun(r); e != nil {
		return r, e
	}
	s.runs[r.ID] = r
	if r.Status == "skipped" {
		return r, nil
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.running[r.ID] = active{cancel: cancel}
	limit := s.settings.LogLimitBytes
	s.wg.Add(1)
	go s.execute(ctx, t, r, limit)
	return r, nil
}

func (s *Service) execute(ctx context.Context, t common.Task, r common.Run, limit int64) {
	defer s.wg.Done()
	env := os.Environ()
	for k, v := range t.Env {
		env = append(env, k+"="+v)
	}
	proc, e := integrations.Start(ctx, integrations.Spec{Executable: t.Executable, Args: t.Args, Cwd: t.Cwd, Env: env, TimeoutMS: t.TimeoutMS, LogLimit: limit}, r.LogPath)
	s.mu.Lock()
	if e == nil && proc.PID > 0 {
		r.Status = "running"
		now := time.Now().UTC()
		r.StartedAt = &now
		s.runs[r.ID] = r
		if err := s.store.SaveRun(r); err != nil {
			s.lastError = err.Error()
			proc.Stop()
		}
	}
	s.mu.Unlock()
	result := integrations.Result{Status: "failed", ErrorCode: "START_FAILED"}
	if e != nil {
		result.Error = e.Error()
		if ctx.Err() != nil {
			result.Status = "canceled"
			result.ErrorCode = "CANCELED"
		}
	} else {
		result = <-proc.Done
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if s.running[r.ID].cancelRequested && result.Status != "interrupted" {
		result.Status = "canceled"
		result.ErrorCode = "CANCELED"
		result.Error = "Run stopped by user or App exit"
	}
	r.Status = result.Status
	r.ExitCode = result.ExitCode
	r.ErrorCode = result.ErrorCode
	r.Error = result.Error
	r.Truncated = result.Truncated
	r.FinalizedAt = &now
	if r.StartedAt != nil && r.Status != "interrupted" {
		r.FinishedAt = &now
		duration := now.Sub(*r.StartedAt).Milliseconds()
		r.DurationMS = &duration
	}
	if err := s.store.SaveRun(r); err != nil {
		s.lastError = err.Error()
	}
	s.runs[r.ID] = r
	s.running[r.ID].cancel()
	delete(s.running, r.ID)
	if r.Status == "failed" {
		event := common.AttentionEvent{ID: common.ID(), SourceType: "task", SourceID: t.ID, RunID: r.ID, Kind: "task_failed", Summary: t.Name + ": " + r.Error, CreatedAt: now}
		if err := s.store.SaveEvent(event); err != nil {
			s.lastError = err.Error()
		}
	}
}

func (s *Service) Runs(task string) []common.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, e := s.taskLocked(task); e == nil {
		task = t.ID
	}
	out := []common.Run{}
	for _, r := range s.runs {
		if task == "" || task == r.TaskID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *Service) Run(id string) (common.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return r, common.Err("NOT_FOUND", "Run not found")
	}
	return r, nil
}

func (s *Service) StopRun(id string) (common.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return r, common.Err("NOT_FOUND", "Run not found")
	}
	if a, ok := s.running[id]; ok {
		a.cancelRequested = true
		s.running[id] = a
		a.cancel()
	}
	return r, nil
}
