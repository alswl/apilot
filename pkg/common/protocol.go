package common

import (
	"encoding/json"
	"errors"
)

const ProtocolVersion = 1

type Request struct {
	Version int             `json:"protocol_version"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type Response struct {
	Version int    `json:"protocol_version"`
	ID      string `json:"id,omitempty"`
	Data    any    `json:"data"`
	Error   *Error `json:"error"`
}

func ProtocolError(e error) *Error {
	var v *Error
	if errors.As(e, &v) {
		return v
	}
	return Err("INTERNAL", e.Error())
}
