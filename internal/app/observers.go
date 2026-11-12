package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alswl/apilot/internal/model"
	"github.com/alswl/apilot/internal/runner"
)

func publicObserver(v model.Observer) model.Observer {
	v.EnvKeys = []string{}
	for k := range v.Env {
		v.EnvKeys = append(v.EnvKeys, k)
	}
	sort.Strings(v.EnvKeys)
	v.Env = nil
	return v
}
func (s *Service) Observers() []model.Observer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []model.Observer{}
	for _, v := range s.observers {
		out = append(out, publicObserver(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
func (s *Service) observerLocked(ref string) (model.Observer, error) {
	if v, ok := s.observers[ref]; ok {
		return v, nil
	}
	for _, v := range s.observers {
		if v.Name == ref {
			return v, nil
		}
	}
	return model.Observer{}, model.Err("NOT_FOUND", "Observer not found")
}
func (s *Service) Observer(ref string) (model.Observer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, e := s.observerLocked(ref)
	return publicObserver(v), e
}
func (s *Service) SaveObserver(ref string, in model.ObserverInput, expected int64) (model.Observer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := model.Observer{ID: ID(), Health: "unknown", Env: map[string]string{}}
	if s.stopping {
		return v, model.Err("CONFLICT", "App is stopping")
	}
	if ref != "" {
		var e error
		v, e = s.observerLocked(ref)
		if e != nil {
			return v, e
		}
		if expected != 0 && expected != v.Revision {
			return v, model.Err("STALE_REVISION", "Observer changed")
		}
	}
	if strings.TrimSpace(in.Name) == "" || in.Executable == "" || !filepath.IsAbs(in.Cwd) || in.IntervalMS < 1000 || in.TimeoutMS < 1 || (in.OutputMode != "text" && in.OutputMode != "json") {
		return v, model.Err("INVALID_ARGUMENT", "Observer needs name, command, absolute directory, interval >=1s and text/json mode")
	}
	for _, other := range s.observers {
		if other.ID != v.ID && other.Name == in.Name {
			return v, model.Err("CONFLICT", "Observer name already exists")
		}
	}
	env := map[string]string{}
	for k, x := range v.Env {
		env[k] = x
	}
	for k, x := range in.EnvSet {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(x, 0) {
			return v, model.Err("INVALID_ARGUMENT", "Invalid environment variable")
		}
		env[k] = x
	}
	for _, k := range in.EnvUnset {
		delete(env, k)
	}
	v.Env = env
	v.Name = in.Name
	v.Executable = in.Executable
	v.Args = in.Args
	v.Cwd = in.Cwd
	v.IntervalMS = in.IntervalMS
	v.TimeoutMS = in.TimeoutMS
	v.Enabled = in.Enabled
	v.OutputMode = in.OutputMode
	v.Revision++
	if e := s.store.SaveObserver(v); e != nil {
		return v, e
	}
	s.observers[v.ID] = v
	s.observerNext[v.ID] = time.Now().Add(time.Duration(v.IntervalMS) * time.Millisecond)
	return publicObserver(v), nil
}
func (s *Service) SetObserverEnabled(ref string, on bool) (model.Observer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return model.Observer{}, model.Err("CONFLICT", "App is stopping")
	}
	v, e := s.observerLocked(ref)
	if e != nil {
		return v, e
	}
	v.Enabled = on
	v.Revision++
	if a, ok := s.observing[v.ID]; ok && !on {
		a.cancel()
	}
	if e = s.store.SaveObserver(v); e != nil {
		return v, e
	}
	s.observers[v.ID] = v
	s.observerNext[v.ID] = time.Now().Add(time.Duration(v.IntervalMS) * time.Millisecond)
	return publicObserver(v), nil
}
func (s *Service) DeleteObserver(ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return model.Err("CONFLICT", "App is stopping")
	}
	v, e := s.observerLocked(ref)
	if e != nil {
		return e
	}
	if a, ok := s.observing[v.ID]; ok {
		a.cancel()
	}
	if e = s.store.DeleteObserver(v.ID); e != nil {
		return e
	}
	delete(s.observers, v.ID)
	delete(s.observerNext, v.ID)
	return nil
}
func (s *Service) pollObservers(now time.Time) {
	if s.stopping || s.suspended {
		return
	}
	if len(s.observing) > 0 {
		return
	}
	for id, v := range s.observers {
		if !v.Enabled || s.observerNext[id].After(now) {
			continue
		}
		s.observerNext[id] = now.Add(time.Duration(v.IntervalMS) * time.Millisecond)
		ctx, cancel := context.WithCancel(s.ctx)
		s.observing[id] = active{cancel: cancel}
		s.wg.Add(1)
		go s.observe(ctx, v)
		return
	}
}
func (s *Service) observe(ctx context.Context, v model.Observer) {
	defer s.wg.Done()
	dir := filepath.Join(s.dir, "observer-logs", v.ID)
	env := os.Environ()
	for k, x := range v.Env {
		env = append(env, k+"="+x)
	}
	p, e := runner.Start(ctx, runner.Spec{Executable: v.Executable, Args: v.Args, Cwd: v.Cwd, Env: env, TimeoutMS: v.TimeoutMS, LogLimit: 65536}, dir)
	health, problem := "healthy", ""
	var result runner.Result
	if e != nil {
		health = "unhealthy"
		problem = e.Error()
	} else {
		result = <-p.Done
		if result.Status != "succeeded" {
			health = "unhealthy"
			problem = result.Error
			if problem == "" {
				problem = result.ErrorCode
			}
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "stdout.log"))
	if v.OutputMode == "json" && (!json.Valid(b) || result.Truncated) {
		health = "unhealthy"
		problem = "Observer returned invalid or truncated JSON"
	}
	text := string(b)
	if len(text) > 65536 {
		text = text[:65536]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	wasCanceled := ctx.Err() != nil
	s.observing[v.ID].cancel()
	delete(s.observing, v.ID)
	if s.stopping || wasCanceled {
		return
	}
	current, ok := s.observers[v.ID]
	if !ok || current.Revision != v.Revision {
		return
	}
	now := time.Now().UTC()
	previous := current.Health
	current.Health = health
	current.Error = problem
	current.Result = text
	current.LastCheckedAt = &now
	current.LastExitCode = result.ExitCode
	current.Truncated = result.Truncated
	if e = s.store.SaveObserver(current); e != nil {
		s.lastError = e.Error()
		return
	}
	s.observers[v.ID] = current
	if previous != health && (previous != "unknown" || health == "unhealthy") {
		event := model.AttentionEvent{ID: ID(), SourceType: "observer", SourceID: v.ID, Kind: "observer_changed", Summary: v.Name + ": " + health, CreatedAt: now}
		if e = s.store.SaveEvent(event); e != nil {
			s.lastError = e.Error()
		}
	}
}
