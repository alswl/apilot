package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/alswl/apilot/core/pkg/config"
)

func TestGitHubConnectionCLIUsesWriteOnlyJSONInput(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "apilot-cli-github-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("APILOT_DATA_DIR", dir)
	socket := config.HTTPSocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(socket))
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []map[string]any
	var operations []map[string]any
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/v1/workbench/task-operations" {
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				http.Error(w, "invalid JSON", 400)
				return
			}
			mu.Lock()
			operations = append(operations, input)
			mu.Unlock()
			fmt.Fprint(w, `{"request_id":"operation","connection_id":"repo-id","sync_state":"accepted"}`)
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/v1/workbench/connections" {
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				http.Error(w, "invalid JSON", 400)
				return
			}
			mu.Lock()
			requests = append(requests, input)
			n := len(requests)
			mu.Unlock()
			if n == 3 {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"detail":"Repository is already connected","errors":[{"location":"code","message":"CONFLICT"},{"location":"connection_id","value":"repo-id"}]}`)
				return
			}
			fmt.Fprintf(w, `{"id":"repo-id","version":%d,"kind":"github_repository","name":"Repo","repository":"owner/repo","mode":"live","capabilities":{"read":true}}`, n)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/workbench/connections/repo-id" {
			fmt.Fprint(w, `{"id":"repo-id","version":2,"kind":"github_repository","name":"Repo","repository":"owner/repo","mode":"live","capabilities":{"read":true}}`)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	bin := filepath.Join(t.TempDir(), "apilot")
	build := exec.Command("go", "build", "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %s %v", output, err)
	}
	call := func(input string, args ...string) ([]byte, error) {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"--json"}, args...)...)
		cmd.Stdin = strings.NewReader(input)
		return cmd.CombinedOutput()
	}
	const token = "test-token-never-returned"
	created, err := call(`{"kind":"github_repository","name":"Repo","repository":"owner/repo","github_token":"`+token+`","write_enabled":true}`, "workbench", "connection", "save", "-")
	if err != nil || bytes.Contains(created, []byte(token)) {
		t.Fatalf("create failed or returned credential: %s %v", created, err)
	}
	edited, err := call(`{"id":"repo-id","version":1,"kind":"github_repository","name":"Repo","repository":"owner/repo"}`, "workbench", "connection", "save", "-")
	if err != nil || bytes.Contains(edited, []byte(token)) {
		t.Fatalf("edit failed or returned credential: %s %v", edited, err)
	}
	shown, err := call("", "workbench", "connection", "show", "repo-id")
	if err != nil || bytes.Contains(shown, []byte(token)) || !bytes.Contains(shown, []byte("owner/repo")) {
		t.Fatalf("readback: %s %v", shown, err)
	}
	duplicate, err := call(`{"kind":"github_repository","name":"Repo","repository":"owner/repo","github_token":"`+token+`"}`, "workbench", "connection", "save", "-")
	if err == nil || !bytes.Contains(duplicate, []byte("repo-id")) || bytes.Contains(duplicate, []byte(token)) {
		t.Fatalf("duplicate result: %s %v", duplicate, err)
	}
	for _, input := range []string{`{"request_id":"old","connection_id":"repo-id","title":"Legacy"}`, `{"request_id":"new","connection_id":"repo-id","title":"Issue","body":"Issue details"}`} {
		if output, err := call(input, "workbench", "task", "operation", "submit", "-"); err != nil {
			t.Fatalf("submit: %s %v", output, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 || requests[0]["github_token"] != token || requests[1]["github_token"] != nil || requests[1]["id"] != "repo-id" {
		t.Fatalf("incorrect CLI request sequence: %+v", requests)
	}
	if len(operations) != 2 || operations[0]["body"] != nil || operations[1]["body"] != "Issue details" {
		t.Fatalf("incorrect operation inputs: %+v", operations)
	}
}
