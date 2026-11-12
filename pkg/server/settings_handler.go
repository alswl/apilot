package server

import (
	"encoding/json"
	"fmt"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchSettings(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "settings.get":
		var p struct{}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		return s.Settings(), nil
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
	default:
		return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
	}
}
