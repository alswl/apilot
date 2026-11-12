package server

import (
	"fmt"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func dispatchAttentionEvent(s *services.Service, r Request) (any, error) {
	switch r.Method {
	case "event.list":
		return dispatchList(s, r)
	case "event.ack":
		var p struct {
			ID string `json:"event_id"`
		}
		if e := decode(r.Params, &p); e != nil {
			return nil, e
		}
		return s.AckEvent(p.ID)
	default:
		return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
	}
}
