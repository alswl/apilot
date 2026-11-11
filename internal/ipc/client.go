package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"github.com/alswl/apilot/internal/model"
	"io"
	"net"
	"os"
	"syscall"
	"time"
)

func Call(path, method string, params any) (json.RawMessage, error) {
	c, e := net.DialTimeout("unix", path, 500*time.Millisecond)
	if e != nil {
		if errors.Is(e, os.ErrNotExist) || errors.Is(e, syscall.ECONNREFUSED) {
			return nil, model.Err("APP_NOT_RUNNING", "Open Apilot.app and retry")
		}
		return nil, model.Err("IPC_UNAVAILABLE", e.Error())
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	b, e := json.Marshal(params)
	if e != nil {
		return nil, e
	}
	if string(b) == "null" {
		b = []byte(`{}`)
	}
	id := "cli"
	if e = json.NewEncoder(c).Encode(Request{Version: Version, ID: id, Method: method, Params: b}); e != nil {
		return nil, model.Err("IPC_UNAVAILABLE", e.Error())
	}
	var r struct {
		Version int             `json:"protocol_version"`
		ID      string          `json:"id"`
		Data    json.RawMessage `json:"data"`
		Error   *model.Error    `json:"error"`
	}
	reader := bufio.NewReaderSize(io.LimitReader(c, 16*1024*1024), 65536)
	line, e := reader.ReadBytes('\n')
	if e != nil {
		if n, ok := e.(net.Error); ok && n.Timeout() {
			return nil, model.Err("IPC_TIMEOUT", "App did not respond in time")
		}
		return nil, model.Err("IPC_UNAVAILABLE", e.Error())
	}
	if e = json.Unmarshal(line, &r); e != nil {
		return nil, model.Err("IPC_UNAVAILABLE", "Invalid App response")
	}
	if r.Version != Version || (r.ID != id && r.Error == nil) {
		return nil, model.Err("PROTOCOL_MISMATCH", "Unexpected App protocol or request ID")
	}
	if r.Error != nil {
		return nil, r.Error
	}
	return r.Data, nil
}
