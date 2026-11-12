package services

import (
	"encoding/json"

	"github.com/alswl/apilot/pkg/integrations"
)

type Remote struct{ socket string }

func NewRemote(socket string) *Remote { return &Remote{socket: socket} }
func (r *Remote) Call(method string, params any) (json.RawMessage, error) {
	return integrations.Call(r.socket, method, params)
}
