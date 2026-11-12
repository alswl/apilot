package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/integrations"
)

type active struct {
	cancel          context.CancelFunc
	cancelRequested bool
}

type Service struct {
	observers    map[string]common.Observer
	observing    map[string]active
	observerNext map[string]time.Time
	closeOnce    sync.Once
	closeError   error
	suspended    bool
	mu           sync.Mutex
	store        Store
	dir          string
	lock         io.Closer
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	tasks        map[string]common.Task
	runs         map[string]common.Run
	running      map[string]active
	settings     common.Settings
	stopping     bool
	lastError    string
}

func New(dir string, openStore func(string) (Store, error)) (*Service, error) {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	if e := os.Chmod(dir, 0700); e != nil {
		return nil, e
	}
	lock, e := integrations.LockFile(filepath.Join(dir, "app.lock"))
	if errors.Is(e, integrations.ErrLocked) {
		return nil, common.Err("CONFLICT", "apilot is already running for this data directory")
	}
	if e != nil {
		return nil, e
	}

	db, e := openStore(filepath.Join(dir, "apilot.db"))
	if e != nil {
		lock.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{observers: map[string]common.Observer{}, observing: map[string]active{}, observerNext: map[string]time.Time{}, store: db, dir: dir, lock: lock, ctx: ctx, cancel: cancel, tasks: map[string]common.Task{}, runs: map[string]common.Run{}, running: map[string]active{}}
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

// StopAdmission also cancels managed work before waiting for IPC or UI workers.
func (s *Service) StopAdmission() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	s.stopping = true
	s.cancel()
	for id, a := range s.running {
		a.cancelRequested = true
		s.running[id] = a
		a.cancel()
	}
}

func (s *Service) Close() error {
	s.StopAdmission()
	s.closeOnce.Do(func() {
		s.wg.Wait()
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closeError = s.store.Close()
		s.lock.Close()
	})
	return s.closeError
}

func (s *Service) Suspend() { s.mu.Lock(); s.suspended = true; s.mu.Unlock() }

func (s *Service) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var events []common.AttentionEvent
	if !s.stopping {
		events, _ = s.store.Events()
	}
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
	return map[string]any{"app_version": common.Version, "protocol_version": 1, "state": state, "running_count": len(s.running), "observer_running_count": len(s.observing), "attention_count": n, "last_error": s.lastError}
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
