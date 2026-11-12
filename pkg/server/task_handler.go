package server

import (
	"encoding/json"
	"fmt"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchTask(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "task.create":
		var p common.TaskInput
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
		in := common.TaskInput{Name: t.Name, Description: t.Description, Executable: t.Executable, Args: t.Args, Cwd: t.Cwd, Enabled: t.Enabled, TimeoutMS: t.TimeoutMS, Schedule: t.Schedule}
		if e = decode(p.Patch, &in); e != nil {
			return nil, e
		}
		return s.SaveTask(p.Task, in, p.Expected)
	case "task.list":
		return dispatchList(s, r)
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
	default:
		return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
	}
}
