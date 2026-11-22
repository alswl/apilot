package services

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestRemoteGitHubConnectionCreateEditAndRead(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "apilot-remote-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	listener, err := net.Listen("unix", filepath.Join(dir, "http.sock"))
	if err != nil {
		t.Fatal(err)
	}
	var requests []map[string]any
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workbench/github-repositories":
			if r.URL.Query().Get("authorization_id") != "approved-session" {
				t.Error("repository listing did not forward the approved session")
			}
			fmt.Fprint(w, `{"items":["owner/repo","owner/second"]}`)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/workbench/connections":
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				http.Error(w, "bad input", 400)
				return
			}
			requests = append(requests, input)
			fmt.Fprintf(w, `{"id":"repo","version":%d,"kind":"github_repository","name":"Repo","repository":"owner/repo","mode":"live"}`, len(requests))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/workbench/connections":
			fmt.Fprint(w, `{"items":[{"id":"repo","version":2,"kind":"github_repository","name":"Repo","repository":"owner/repo","mode":"live"}]}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	remote := NewRemote(filepath.Join(dir, "legacy.sock"))
	repositories, err := remote.ListGitHubRepositories("approved-session")
	if err != nil || !slices.Equal(repositories, []string{"owner/repo", "owner/second"}) {
		t.Fatalf("list authorized repositories: %v, %v", repositories, err)
	}
	connection := common.WorkbenchConnection{Kind: common.WorkbenchGitHubRepository, Name: "Repo", Repository: "owner/repo"}
	created, err := remote.ConfigureWorkbenchGitHub(connection, "test-secret", true)
	if err != nil || created.ID != "repo" {
		t.Fatalf("create: %+v %v", created, err)
	}
	connection.ID, connection.Version = created.ID, created.Version
	edited, err := remote.ConfigureWorkbenchGitHub(connection, "", true)
	if err != nil || edited.Version != 2 {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	snapshots, err := remote.WorkbenchConnections()
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != "repo" {
		t.Fatalf("read: %+v %v", snapshots, err)
	}
	raw, err := json.Marshal(snapshots)
	if err != nil || strings.Contains(string(raw), "test-secret") {
		t.Fatal("credential returned in snapshot")
	}
	if len(requests) != 2 || requests[0]["github_token"] != "test-secret" || requests[1]["github_token"] != nil || requests[1]["id"] != "repo" {
		t.Fatalf("unexpected remote requests: %+v", requests)
	}
}
