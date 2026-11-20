package services

import (
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

// This opt-in measurement uses a local bridge fixture, not an Obsidian process.
func TestWorkbenchBridgeOperationMeasurement(t *testing.T) {
	if os.Getenv("APILOT_BRIDGE_MEASUREMENT_TEST") != "1" {
		t.Skip("set APILOT_BRIDGE_MEASUREMENT_TEST=1 to measure bridge operations")
	}
	const directories, notesPerDirectory = 100, 20
	vault := t.TempDir()
	for i := range directories {
		dir := filepath.Join(vault, fmt.Sprintf("folder-%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for j := range notesPerDirectory {
			content := fmt.Sprintf("# Project\n- [ ] Task %d-%d 🆔 task-%d-%d\n", i, j, i, j)
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note-%02d.md", j)), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/vault/")
		if !filepath.IsLocal(path) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		path = filepath.Join(vault, path)
		data, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tag := `"` + workbenchFingerprint(string(data)) + `"`
		if r.Method == http.MethodGet {
			w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
			w.Header().Set("ETag", tag)
			_, _ = w.Write(data)
			return
		}
		if r.Method != http.MethodPut || r.Header.Get("If-Match") != tag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		data, err = io.ReadAll(r.Body)
		if err == nil {
			err = os.WriteFile(path, data, 0600)
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer bridge.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	enabled := true
	id, err := s.ConfigureWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Fixture", Vault: vault}, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "test-token", Certificate: string(cert), WriteEnabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		snapshot, err := s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.LastSuccess != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("initial read did not finish: %+v", snapshot.Attempts)
		}
		time.Sleep(10 * time.Millisecond)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := store.WorkbenchObjects(id)
	if err != nil || len(tasks) != directories*notesPerDirectory {
		t.Fatalf("initial tasks: %d %v", len(tasks), err)
	}
	var target string
	for _, task := range tasks {
		if task.NativeID == "task-50-10" {
			target = task.ID
		}
	}
	if target == "" {
		t.Fatal("target task missing")
	}
	for i, state := range []string{"done", "open", "done", "open"} {
		task, err := s.WorkbenchTask(target)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		receipt, err := s.SubmitWorkbenchTaskOperation(common.WorkbenchTaskWrite{RequestID: fmt.Sprintf("measure-%d", i), ConnectionID: id, ConnectionVersion: task.ConnectionVersion, SourceObjectID: task.ID, TargetVersion: task.TargetVersion, State: state})
		accepted := time.Since(start)
		if err != nil || receipt.SyncState != common.WorkbenchOperationAccepted {
			t.Fatalf("admission: %+v %v", receipt, err)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			receipt, err = s.WorkbenchTaskOperationReceipt(receipt.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.SyncState == common.WorkbenchOperationConfirmed {
				break
			}
			if receipt.SyncState == common.WorkbenchOperationConflict || receipt.SyncState == common.WorkbenchOperationNotExecuted || time.Now().After(deadline) {
				t.Fatalf("operation did not confirm: %+v", receipt)
			}
			time.Sleep(10 * time.Millisecond)
		}
		confirmed := time.Since(start)
		data, err := os.ReadFile(filepath.Join(vault, "folder-050", "note-10.md"))
		checkbox := "- [ ] Task 50-10"
		if state == "done" {
			checkbox = "- [x] Task 50-10"
		}
		if err != nil || !strings.Contains(string(data), checkbox) || receipt.SourceObjectID != target {
			t.Fatalf("source or identity did not match: %q %+v %v", data, receipt, err)
		}
		t.Logf("sample=%d state=%s admission=%s confirmation=%s", i+1, state, accepted.Round(time.Millisecond), confirmed.Round(time.Millisecond))
	}
}
