package services

import (
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func TestWorkbenchObsidianCreateThroughBridge(t *testing.T) {
	vault := t.TempDir()
	note := filepath.Join(vault, "List.md")
	if err := os.WriteFile(note, []byte("# A\n## Tasks\n### Child\n# B\n## Tasks\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var competingSave atomic.Bool
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := filepath.Join(vault, strings.TrimPrefix(r.URL.Path, "/vault/"))
		data, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		tag := "\"" + workbenchFingerprint(string(data)) + "\""
		if r.Method == http.MethodGet {
			w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
			w.Header().Set("ETag", tag)
			_, _ = w.Write(data)
			return
		}
		if competingSave.Swap(false) {
			changed := strings.Replace(string(data), "Bridge task", "Bridge task edited at source", 1)
			changed += "\nSource editor saved this paragraph.\n"
			if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			tag = "\"" + workbenchFingerprint(changed) + "\""
		}
		if r.Header.Get("If-Match") != tag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		data, err = io.ReadAll(r.Body)
		if err == nil {
			err = os.WriteFile(path, data, 0600)
		}
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// The bridge wrote the note, but its response cannot confirm success.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bridge.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	enabled := true
	connectionID, err := s.ConfigureWorkbench(common.WorkbenchConnection{
		Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault,
	}, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "test-token", Certificate: string(certificate), WriteEnabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		connection, err := s.WorkbenchSnapshot(connectionID)
		if err != nil {
			t.Fatal(err)
		}
		if connection.LastSuccess != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("initial scan did not finish")
		case <-time.After(5 * time.Millisecond):
		}
	}
	headings, err := s.WorkbenchHeadings(connectionID, "List.md")
	if err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{
		RequestID: "create-once", ConnectionID: connectionID, ConnectionVersion: 1,
		NotePath: "List.md", HeadingPath: headings[1].HeadingPath, TargetVersion: headings[1].TargetVersion,
		Title: "Bridge task",
	}
	receipt, err := s.WriteWorkbenchTask(input)
	if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID == "" {
		t.Fatalf("create: %+v %v", receipt, err)
	}
	replayed, err := s.WriteWorkbenchTask(input)
	if err != nil || replayed != receipt {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
	data, err := os.ReadFile(note)
	if err != nil || strings.Count(string(data), "- [ ] Bridge task") != 1 ||
		!strings.Contains(string(data), "# A\n## Tasks\n- [ ] Bridge task") {
		t.Fatalf("note: %s %v", data, err)
	}
	task, err := s.WorkbenchTask(receipt.SourceObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Activity) != 1 || task.Activity[0].Time.IsZero() || task.Activity[0].Text != "从 Obsidian 导入" {
		t.Fatalf("missing import activity: %+v", task.Activity)
	}
	importedAt := task.Activity[0].Time
	beforeComplete := string(data)
	status := common.WorkbenchTaskWrite{
		RequestID: "complete-bridge-task", SourceObjectID: task.ID, State: "done",
		ConnectionVersion: task.ConnectionVersion, TargetVersion: task.TargetVersion,
	}
	completed, err := s.WriteWorkbenchTask(status)
	if err != nil || completed.SyncState != "confirmed" || completed.SourceObjectID != task.ID {
		t.Fatalf("complete: %+v %v", completed, err)
	}
	data, err = os.ReadFile(note)
	want := strings.Replace(beforeComplete, "- [ ] Bridge task", "- [x] Bridge task", 1)
	want = strings.Replace(want, obsidianCreationMarker(workbenchFingerprint(input))+"\n", obsidianCreationMarker(workbenchFingerprint(input))+" ✅ "+time.Now().Format(time.DateOnly)+"\n", 1)
	if err != nil || string(data) != want {
		t.Fatalf("completion modified more than the checkbox and date: %s %v", data, err)
	}
	if replay, err := s.WriteWorkbenchTask(status); err != nil || replay != completed {
		t.Fatalf("complete replay: %+v %v", replay, err)
	}
	task, err = s.WorkbenchTask(task.ID)
	if err != nil || task.State != "done" || len(task.Activity) != 1 || !task.Activity[0].Time.Equal(importedAt) {
		t.Fatalf("completed task: %+v %v", task, err)
	}
	status.RequestID, status.State, status.TargetVersion = "reopen-bridge-task", "open", task.TargetVersion
	reopened, err := s.WriteWorkbenchTask(status)
	if err != nil || reopened.SyncState != "confirmed" || reopened.SourceObjectID != task.ID {
		t.Fatalf("reopen: %+v %v", reopened, err)
	}
	data, err = os.ReadFile(note)
	if err != nil || string(data) != beforeComplete {
		t.Fatalf("reopen changed other note content: %s %v", data, err)
	}
	task, err = s.WorkbenchTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	status.RequestID, status.State, status.TargetVersion = "competing-state-write", "done", task.TargetVersion
	competingSave.Store(true)
	conflict, err := s.WriteWorkbenchTask(status)
	if err != nil || conflict.SyncState != "conflict" {
		t.Fatalf("competing save: %+v %v", conflict, err)
	}
	data, err = os.ReadFile(note)
	want = strings.Replace(beforeComplete, "Bridge task", "Bridge task edited at source", 1) + "\nSource editor saved this paragraph.\n"
	if err != nil || string(data) != want {
		t.Fatalf("competing save was not preserved: %q %v", data, err)
	}
	if replay, err := s.WriteWorkbenchTask(status); err != nil || replay != conflict {
		t.Fatalf("conflict replay: %+v %v", replay, err)
	}
	connection, err := s.WorkbenchSnapshot(connectionID)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := s.ConfigureWorkbench(connection.WorkbenchConnection, common.WorkbenchBridgeInput{WriteEnabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	status.RequestID = "write-after-revocation"
	if _, err := s.WriteWorkbenchTask(status); err == nil {
		t.Fatal("revoked connection allowed stale task write")
	}
	data, err = os.ReadFile(note)
	if err != nil || string(data) != want {
		t.Fatalf("write after revocation modified the note: %q %v", data, err)
	}

}
