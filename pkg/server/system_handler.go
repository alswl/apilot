package server

import (
	"fmt"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchSystem(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "status", "doctor":
		var p struct{}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		if r.Method == "status" {
			return s.Status(), nil
		}
		return s.Doctor(), nil
	default:
		return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
	}
}
