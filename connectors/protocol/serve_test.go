package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeSingleRequest(t *testing.T) {
	args := []string{"serve", "--protocol", "1"}
	description := Description{ConnectorID: "fixture", ReleaseVersion: "0.1.0", ProtocolVersions: []int{Version}}
	cases := []struct {
		method string
		kind   ErrorKind
	}{
		{"describe", ""},
		{"tasks.sync", Unsupported},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			request, err := json.Marshal(Request{JSONRPC: "2.0", ID: "req", Method: tc.method})
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := Serve(context.Background(), args, strings.NewReader(string(request)+"\n"), &output, description, nil); err != nil {
				t.Fatal(err)
			}
			var response Message
			if err := json.Unmarshal(output.Bytes(), &response); err != nil || response.ID == nil || *response.ID != "req" {
				t.Fatalf("invalid response: %s (%v)", &output, err)
			}
			if tc.kind != "" {
				if response.Error == nil || response.Error.Data.Kind != tc.kind {
					t.Fatalf("unexpected error: %s", &output)
				}
			} else {
				var got Description
				if err := json.Unmarshal(response.Result, &got); err != nil || got.ConnectorID != description.ConnectorID {
					t.Fatalf("unexpected description: %s (%v)", &output, err)
				}
			}
		})
	}
	var output bytes.Buffer
	twoFrames := strings.Repeat(`{"jsonrpc":"2.0","id":"req","method":"describe"}`+"\n", 2)
	if err := Serve(context.Background(), args, strings.NewReader(twoFrames), &output, description, nil); err != ErrFrame || output.Len() != 0 {
		t.Fatalf("expected one request only: %v %s", err, &output)
	}
}
