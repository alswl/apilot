package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/alswl/apilot/internal/model"
	"github.com/alswl/apilot/internal/runner"
	"github.com/alswl/apilot/internal/schedule"
	"github.com/alswl/apilot/internal/storage"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type active struct {
	cancel          context.CancelFunc
	cancelRequested bool
}
type Service struct {
	observers    map[string]model.Observer
	observing    map[string]active
	observerNext map[string]time.Time
	closeOnce    sync.Once
	closeError   error
	suspended    bool
	mu           sync.Mutex
	store        *storage.Store
	dir          string
	lock         *os.File
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	tasks        map[string]model.Task
	runs         map[string]model.Run
	running      map[string]active
	settings     model.Settings
	stopping     bool
	lastError    string
}

func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func DataDir() string {
	if d := os.Getenv("APILOT_DATA_DIR"); d != "" {
		p, _ := filepath.Abs(d)
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "apilot")
}
func SocketPath(dir string) string {
	cache, _ := os.UserCacheDir()
	hash := sha256.Sum256([]byte(dir))
	return filepath.Join(cache, "apilot", hex.EncodeToString(hash[:5]), "ipc.sock")
}
func Open(dir string) (*Service, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return nil, e
	}
	lock, e := os.OpenFile(filepath.Join(dir, "app.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		lock.Close()
		return nil, model.Err("CONFLICT", "apilot is already running for this data directory")
	}
	db, e := storage.Open(filepath.Join(dir, "apilot.db"))
	if e != nil {
		lock.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{observers: map[string]model.Observer{}, observing: map[string]active{}, observerNext: map[string]time.Time{}, store: db, dir: dir, lock: lock, ctx: ctx, cancel: cancel, tasks: map[string]model.Task{}, runs: map[string]model.Run{}, running: map[string]active{}}
	fail := func(e error) (*Service, error) { cancel(); db.Close(); lock.Close(); return nil, e }
	s.settings, e = db.Settings()
	if e != nil {
		return fail(e)
	}
	ts, e := db.Tasks()
	if e != nil {
		return fail(e)
	}
	now := time.Now().UTC()
	for _, t := range ts {
		if e = s.resetSchedule(&t, now); e != nil {
			return fail(e)
		}
		s.tasks[t.ID] = t
	}
	rs, e := db.Runs()
	if e != nil {
		return fail(e)
	}
	for _, r := range rs {
		if r.Active() {
			r.Status = "interrupted"
			r.FinalizedAt = &now
			r.FinishedAt = nil
			r.DurationMS = nil
			r.ErrorCode = "APP_INTERRUPTED"
			r.Error = "App ended before the run was finalized"
			if e = db.SaveRun(r); e != nil {
				return fail(e)
			}
		}
		s.runs[r.ID] = r
	}
	observers, e := db.Observers()
	if e != nil {
		return fail(e)
	}
	for _, v := range observers {
		s.observers[v.ID] = v
		s.observerNext[v.ID] = now.Add(time.Duration(v.IntervalMS) * time.Millisecond)
	}
	s.wg.Add(1)
	go s.tick()
	return s, nil
}
func (s *Service) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.stopping = true
		s.cancel()
		for id, a := range s.running {
			a.cancelRequested = true
			s.running[id] = a
			a.cancel()
		}
		s.mu.Unlock()
		s.wg.Wait()
		s.closeError = s.store.Close()
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		s.lock.Close()
	})
	return s.closeError
}
func (s *Service) Suspend() { s.mu.Lock(); s.suspended = true; s.mu.Unlock() }

func (s *Service) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	events, _ := s.store.Events()
	n := 0
	for _, e := range events {
		if e.AcknowledgedAt == nil {
			n++
		}
	}
	state := "ready"
	if s.stopping {
		state = "stopping"
	}
	return map[string]any{"app_version": "0.1.0-dev", "protocol_version": 1, "state": state, "running_count": len(s.running), "observer_running_count": len(s.observing), "attention_count": n, "last_error": s.lastError}
}
func (s *Service) taskLocked(ref string) (model.Task, error) {
	if t, ok := s.tasks[ref]; ok {
		return t, nil
	}
	for _, t := range s.tasks {
		if t.Name == ref {
			return t, nil
		}
	}
	return model.Task{}, model.Err("NOT_FOUND", "Task not found")
}
func public(t model.Task) model.Task {
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
func (s *Service) Tasks() []model.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Task{}
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
func (s *Service) Task(ref string) (model.Task, error) {
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
func (s *Service) SaveTask(ref string, in model.TaskInput, expected int64) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return model.Task{}, model.Err("CONFLICT", "App is stopping")
	}
	now := time.Now().UTC()
	t := model.Task{ID: ID(), CreatedAt: now, Env: map[string]string{}}
	if ref != "" {
		var e error
		t, e = s.taskLocked(ref)
		if e != nil {
			return t, e
		}
		if expected != 0 && t.Revision != expected {
			return t, model.Err("STALE_REVISION", "Task changed; reload before saving")
		}
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 128 || strings.ContainsAny(in.Name, "\r\n\t") {
		return t, model.Err("INVALID_ARGUMENT", "Task name must contain 1–128 characters without control characters")
	}
	if in.Executable == "" || !filepath.IsAbs(in.Cwd) || in.TimeoutMS <= 0 {
		return t, model.Err("INVALID_ARGUMENT", "Executable, absolute working directory and positive timeout are required")
	}
	for _, x := range s.tasks {
		if x.ID != t.ID && x.Name == in.Name {
			return t, model.Err("CONFLICT", "A task with that name already exists")
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
			return t, model.Err("INVALID_ARGUMENT", "Invalid environment variable")
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
			zone, e := schedule.SystemTimezone()
			if e != nil {
				return t, model.Err("INVALID_ARGUMENT", e.Error())
			}
			cp.Timezone = zone
		}
		if cp.ID == "" {
			cp.ID = ID()
		}
		t.Schedule = &cp
	}
	if e := s.resetSchedule(&t, now); e != nil {
		return t, model.Err("INVALID_ARGUMENT", e.Error())
	}
	if e := s.store.SaveTask(t); e != nil {
		return t, e
	}
	s.tasks[t.ID] = t
	return public(t), nil
}
func (s *Service) SetEnabled(ref string, on bool) (model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, e := s.taskLocked(ref)
	if e != nil {
		return t, e
	}
	if s.stopping {
		return t, model.Err("CONFLICT", "App is stopping")
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
	t, e := s.taskLocked(ref)
	if e != nil {
		return e
	}
	for id := range s.running {
		if s.runs[id].TaskID == t.ID {
			return model.Err("CONFLICT", "Stop the active run before deleting this task")
		}
	}
	if e = s.store.DeleteTask(t.ID); e != nil {
		return e
	}
	delete(s.tasks, t.ID)
	return nil
}
func (s *Service) resetSchedule(t *model.Task, now time.Time) error {
	if t.Schedule == nil {
		return nil
	}
	cp := *t.Schedule
	t.Schedule = &cp
	next, e := schedule.Next(t.Schedule, now)
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
func (s *Service) RunTask(ref, trigger string) (model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, e := s.taskLocked(ref)
	if e != nil {
		return model.Run{}, e
	}
	if trigger != "manual" && trigger != "cli" {
		return model.Run{}, model.Err("INVALID_ARGUMENT", "Invalid manual trigger source")
	}
	return s.runLocked(t, trigger, nil)
}
func (s *Service) runLocked(t model.Task, trigger string, scheduledAt *time.Time) (model.Run, error) {
	if s.stopping {
		return model.Run{}, model.Err("CONFLICT", "App is stopping")
	}
	now := time.Now().UTC()
	r := model.Run{ScheduledAt: scheduledAt, ID: ID(), TaskID: t.ID, TaskName: t.Name, Executable: t.Executable, Args: t.Args, Trigger: trigger, Status: "queued", CreatedAt: now}
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
func (s *Service) execute(ctx context.Context, t model.Task, r model.Run, limit int64) {
	defer s.wg.Done()
	env := os.Environ()
	for k, v := range t.Env {
		env = append(env, k+"="+v)
	}
	proc, e := runner.Start(ctx, runner.Spec{Executable: t.Executable, Args: t.Args, Cwd: t.Cwd, Env: env, TimeoutMS: t.TimeoutMS, LogLimit: limit}, r.LogPath)
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
	result := runner.Result{Status: "failed", ErrorCode: "START_FAILED"}
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
		event := model.AttentionEvent{ID: ID(), SourceType: "task", SourceID: t.ID, RunID: r.ID, Kind: "task_failed", Summary: t.Name + ": " + r.Error, CreatedAt: now}
		if err := s.store.SaveEvent(event); err != nil {
			s.lastError = err.Error()
		}
	}
}
func (s *Service) Runs(task string) []model.Run {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, e := s.taskLocked(task); e == nil {
		task = t.ID
	}
	out := []model.Run{}
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
func (s *Service) Run(id string) (model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return r, model.Err("NOT_FOUND", "Run not found")
	}
	return r, nil
}
func (s *Service) StopRun(id string) (model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return r, model.Err("NOT_FOUND", "Run not found")
	}
	if a, ok := s.running[id]; ok {
		a.cancelRequested = true
		s.running[id] = a
		a.cancel()
	}
	return r, nil
}
func (s *Service) Settings() model.Settings { s.mu.Lock(); defer s.mu.Unlock(); return s.settings }
func (s *Service) SaveSettings(v model.Settings) (model.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.MaxConcurrent < len(s.running) {
		return v, model.Err("CONFLICT", "Stop active runs before lowering the limit below the active count")
	}
	if v.DefaultTimeoutMS <= 0 || v.MaxConcurrent < 1 || v.RetentionDays < 1 || v.LogLimitBytes < 1 {
		return v, model.Err("INVALID_ARGUMENT", "Settings values must be positive")
	}
	if v.Revision != s.settings.Revision {
		return v, model.Err("STALE_REVISION", "Settings changed")
	}
	v.Revision++
	if e := s.store.SaveSettings(v); e != nil {
		return v, e
	}
	s.settings = v
	return v, nil
}
func (s *Service) Events() ([]model.AttentionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Events()
}
func (s *Service) AckEvent(id string) (model.AttentionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events, e := s.store.Events()
	if e != nil {
		return model.AttentionEvent{}, e
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
	return model.AttentionEvent{}, model.Err("NOT_FOUND", "Event not found")
}
func (s *Service) Doctor() map[string]any {
	checks := map[string]any{}
	for _, path := range []string{s.dir, filepath.Join(s.dir, "logs")} {
		st, e := os.Stat(path)
		checks[path] = map[string]any{"exists": e == nil, "directory": e == nil && st.IsDir()}
	}
	return map[string]any{"data_dir": s.dir, "schema_version": 2, "status": s.Status(), "path_checks": checks}
}
func (s *Service) String() string { return fmt.Sprintf("apilot(%s)", s.dir) }

func (s *Service) AllowNotification(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.AllowNotification(key, time.Now())
}
func (s *Service) Prune() error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
