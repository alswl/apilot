package server

import (
	"encoding/json"
	"testing"

	"github.com/alswl/apilot/pkg/config"
)

func TestProtocolAndStrictInput(t *testing.T) {
	s, e := OpenService(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, req := range []Request{{Version: 999, Method: "status"}, {Version: 1, Method: "unknown"}, {Version: 1, Method: "status", Params: json.RawMessage(`{"unexpected":true}`)}} {
		if _, e := Dispatch(s, req); e == nil {
			t.Fatal("accepted invalid request", req)
		}
	}
	path := config.SocketPath(t.TempDir())
	server, e := Listen(s, path)
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	b, e := Call(path, "status", nil)
	if e != nil || !json.Valid(b) {
		t.Fatal(string(b), e)
	}
}
