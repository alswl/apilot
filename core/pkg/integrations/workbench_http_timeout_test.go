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
