package server

import (
	"bytes"
	"encoding/json"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
)

func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return common.Err("INVALID_ARGUMENT", e.Error())
	}
	if d.More() {
		return common.Err("INVALID_ARGUMENT", "Unexpected input")
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

const Version = common.ProtocolVersion

type Request = common.Request
type Response = common.Response

func Error(e error) *common.Error { return common.ProtocolError(e) }

func Call(path, method string, params any) (json.RawMessage, error) {
	return services.NewRemote(path).Call(method, params)
}
