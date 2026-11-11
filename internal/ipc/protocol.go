package ipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/model"
)

const Version = 1

type Request struct {
	Version int             `json:"protocol_version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type Response struct {
	Version int          `json:"protocol_version"`
	ID      string       `json:"id,omitempty"`
	Data    any          `json:"data"`
	Error   *model.Error `json:"error"`
}

func Error(e error) *model.Error {
	var v *model.Error
	if errors.As(e, &v) {
		return v
	}
	return model.Err("INTERNAL", e.Error())
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return model.Err("INVALID_ARGUMENT", e.Error())
	}
	if d.More() {
		return model.Err("INVALID_ARGUMENT", "Unexpected input")
	}
	return nil
}
func page[T any](items []T, limit, offset int) any {
	if limit == 0 {
		limit = 100
	}
	if offset < 0 || limit < 1 || limit > 1000 {
		return nil
	}
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return map[string]any{"items": items[offset:end], "total": total, "limit": limit, "offset": offset}
}
func Dispatch(s *app.Service, r Request) (any, error) {
	if r.Version != Version {
		return nil, model.Err("PROTOCOL_MISMATCH", "Expected protocol_version 1")
	}
	if len(r.Params) == 0 {
		r.Params = json.RawMessage(`{}`)
	}
	switch r.Method {
	case "status", "doctor", "settings.get":
		var p struct{}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		if r.Method == "status" {
			return s.Status(), nil
		}
		if r.Method == "doctor" {
			return s.Doctor(), nil
		}
		return s.Settings(), nil
	case "task.create":
		var p model.TaskInput
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		return s.SaveTask("", p, 0)
	case "task.update":
		var p struct {
			Task     string          `json:"task"`
			Patch    json.RawMessage `json:"patch"`
			Expected int64           `json:"expected_revision"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		t, e := s.Task(p.Task)
		if e != nil {
			return nil, e
		}
		in := model.TaskInput{Name: t.Name, Description: t.Description, Executable: t.Executable, Args: t.Args, Cwd: t.Cwd, Enabled: t.Enabled, TimeoutMS: t.TimeoutMS, Schedule: t.Schedule}
		if e = decode(p.Patch, &in); e != nil {
			return nil, e
		}
		return s.SaveTask(p.Task, in, p.Expected)
	case "task.list", "run.list", "event.list":
		var p struct {
			Task   string `json:"task"`
			Limit  int    `json:"limit"`
			Offset int    `json:"offset"`
			Unread bool   `json:"unread"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		if p.Limit < 0 || p.Limit > 1000 || p.Offset < 0 {
			return nil, model.Err("INVALID_ARGUMENT", "Invalid pagination")
		}
		if r.Method == "task.list" {
			return page(s.Tasks(), p.Limit, p.Offset), nil
		}
		if r.Method == "run.list" {
			return page(s.Runs(p.Task), p.Limit, p.Offset), nil
		}
		events, e := s.Events()
		if p.Unread {
			filtered := []model.AttentionEvent{}
			for _, v := range events {
				if v.AcknowledgedAt == nil {
					filtered = append(filtered, v)
				}
			}
			events = filtered
		}
		return page(events, p.Limit, p.Offset), e
	case "task.get", "task.remove", "task.set_enabled", "task.run":
		var p struct {
			Task    string `json:"task"`
			Enabled bool   `json:"enabled"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		switch r.Method {
		case "task.get":
			return s.Task(p.Task)
		case "task.remove":
			return map[string]any{"removed": p.Task}, s.DeleteTask(p.Task)
		case "task.set_enabled":
			return s.SetEnabled(p.Task, p.Enabled)
		default:
			return s.RunTask(p.Task, "cli")
		}
	case "run.get", "run.stop":
		var p struct {
			ID string `json:"run_id"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		if r.Method == "run.get" {
			return s.Run(p.ID)
		}
		return s.StopRun(p.ID)
	case "logs.read":
		var p struct {
			ID     string `json:"run_id"`
			Stream string `json:"stream"`
			Offset int64  `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		if p.Stream == "" {
			p.Stream = "stdout"
		}
		if p.Limit == 0 {
			p.Limit = 65536
		}
		return s.Logs(p.ID, p.Stream, p.Offset, p.Limit)
	case "observer.create":
		var p model.ObserverInput
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		return s.SaveObserver("", p, 0)
	case "observer.update":
		var p struct {
			Observer string          `json:"observer"`
			Patch    json.RawMessage `json:"patch"`
			Expected int64           `json:"expected_revision"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		v, e := s.Observer(p.Observer)
		if e != nil {
			return nil, e
		}
		in := model.ObserverInput{Name: v.Name, Executable: v.Executable, Args: v.Args, Cwd: v.Cwd, IntervalMS: v.IntervalMS, TimeoutMS: v.TimeoutMS, Enabled: v.Enabled, OutputMode: v.OutputMode}
		if e = decode(p.Patch, &in); e != nil {
			return nil, e
		}
		return s.SaveObserver(p.Observer, in, p.Expected)
	case "observer.list":
		var p struct {
			Limit  int `json:"limit"`
			Offset int `json:"offset"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		if p.Limit < 0 || p.Limit > 1000 || p.Offset < 0 {
			return nil, model.Err("INVALID_ARGUMENT", "Invalid pagination")
		}
		return page(s.Observers(), p.Limit, p.Offset), nil
	case "observer.get", "observer.remove", "observer.set_enabled":
		var p struct {
			Observer string `json:"observer"`
			Enabled  bool   `json:"enabled"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		switch r.Method {
		case "observer.get":
			return s.Observer(p.Observer)
		case "observer.remove":
			return map[string]any{"removed": p.Observer}, s.DeleteObserver(p.Observer)
		default:
			return s.SetObserverEnabled(p.Observer, p.Enabled)
		}
	case "settings.update":
		var p struct {
			Patch    json.RawMessage `json:"patch"`
			Expected int64           `json:"expected_revision"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		v := s.Settings()
		if e := decode(p.Patch, &v); e != nil {
			return nil, e
		}
		if p.Expected != 0 {
			v.Revision = p.Expected
		}
		return s.SaveSettings(v)
	case "event.ack":
		var p struct {
			ID string `json:"event_id"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		return s.AckEvent(p.ID)
	default:
		return nil, model.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
	}
}
