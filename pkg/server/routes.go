package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

type handler func(*services.Service, Request) (any, error)

var routes = map[string]handler{
	"task":     dispatchTask,
	"run":      dispatchRun,
	"logs":     dispatchRun,
	"observer": dispatchObserver,
	"settings": dispatchSettings,
	"event":    dispatchAttentionEvent,
	"status":   dispatchSystem,
	"doctor":   dispatchSystem,
}

func Dispatch(s *services.Service, r Request) (any, error) {
	if r.Version != Version {
		return nil, common.Err("PROTOCOL_MISMATCH", "Expected protocol_version 1")
	}
	if len(r.Params) == 0 {
		r.Params = json.RawMessage(`{}`)
	}
	prefix, _, _ := strings.Cut(r.Method, ".")
	if route, ok := routes[prefix]; ok {
		return route(s, r)
	}
	return nil, common.Err("INVALID_ARGUMENT", fmt.Sprintf("Unknown method %q", r.Method))
}
