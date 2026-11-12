package server

import (
	"encoding/json"
	"fmt"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchObserver(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "observer.create":
		var p common.ObserverInput
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
		in := common.ObserverInput{Name: v.Name, Executable: v.Executable, Args: v.Args, Cwd: v.Cwd, IntervalMS: v.IntervalMS, TimeoutMS: v.TimeoutMS, Enabled: v.Enabled, OutputMode: v.OutputMode}
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
			return nil, common.Err("INVALID_ARGUMENT", "Invalid pagination")
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
	default:
		return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
	}
}
