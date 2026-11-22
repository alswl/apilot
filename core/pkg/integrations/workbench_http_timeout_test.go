package integrations

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWorkbenchReconcileAllowsSlowRead(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "apilot-http-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "test.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"request_id":"request","connection_id":"vault","sync_state":"needs_review"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	if _, err := CallWorkbenchHTTP(socket, "workbench.operation.reconcile", map[string]string{"id": "request"}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchConnectionSaveAllowsGitHubVerification(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "apilot-http-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "test.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"repo","kind":"github_repository"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	if _, err := CallWorkbenchHTTP(socket, "workbench.connection.save", map[string]string{"kind": "github_repository", "name": "Repo", "repository": "owner/repo"}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchProblemPreservesSafeDetails(t *testing.T) {
	problem := decodeWorkbenchProblem(http.StatusConflict, []byte(`{"detail":"Repository is already connected","errors":[{"location":"code","message":"CONFLICT"},{"location":"connection_id","value":"existing"},{"location":"github_token","value":"must-not-escape"}]}`))
	if problem.Code != "CONFLICT" || problem.Details["connection_id"] != "existing" {
		t.Fatalf("missing duplicate details: %+v", problem)
	}
	if _, ok := problem.Details["github_token"]; ok {
		t.Fatal("unexpected sensitive error detail")
	}
	problem = decodeWorkbenchProblem(http.StatusTooManyRequests, []byte(`{"detail":"wait","errors":[{"location":"code","message":"RATE_LIMITED"},{"location":"retry_after","value":60}]}`))
	if problem.Code != "RATE_LIMITED" || problem.Details["retry_after"] != float64(60) {
		t.Fatalf("missing retry details: %+v", problem)
	}
}
