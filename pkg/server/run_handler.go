package server

import (
	"fmt"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchRun(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "run.list":
		return dispatchList(s, r)
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
	default:
		return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
	}
}
