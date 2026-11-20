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

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchBridgeWritesStayInSelectedVault(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	original := "# Tasks\n- [ ] Shared 🆔 shared\n"
	type fixture struct{ id, path, taskID string }
	var fixtures []fixture
	for i := range 2 {
		vault := t.TempDir()
		note := filepath.Join(vault, "tasks.md")
		if err := os.WriteFile(note, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Join(vault, strings.TrimPrefix(r.URL.Path, "/vault/"))
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
		certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
		enabled := true
		id, err := s.ConfigureWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: string(rune('A' + i)), Vault: vault}, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(certificate), WriteEnabled: &enabled})
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		var taskID string
		for time.Now().Before(deadline) {
			page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) == 1 {
				taskID = page.Items[0].ID
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if taskID == "" {
			t.Fatal("vault did not synchronize")
		}
		fixtures = append(fixtures, fixture{id: id, path: note, taskID: taskID})
	}
	if fixtures[0].taskID == fixtures[1].taskID {
		t.Fatal("independent vaults shared a task identity")
	}
	for i, fixture := range fixtures {
		task, err := s.WorkbenchTask(fixture.taskID)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			_, err := s.SubmitWorkbenchTaskOperation(common.WorkbenchTaskWrite{RequestID: "wrong-vault", ConnectionID: fixtures[1].id, ConnectionVersion: task.ConnectionVersion, SourceObjectID: task.ID, TargetVersion: task.TargetVersion, State: "done"})
			if err == nil {
				t.Fatal("accepted a task belonging to another connection")
			}
		}
		receipt, err := s.WriteWorkbenchTask(common.WorkbenchTaskWrite{RequestID: "complete-" + fixture.id, ConnectionID: fixture.id, ConnectionVersion: task.ConnectionVersion, SourceObjectID: task.ID, TargetVersion: task.TargetVersion, State: "done"})
		if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID != task.ID {
			t.Fatalf("complete vault %d: %+v %v", i, receipt, err)
		}
		for j, other := range fixtures {
			data, err := os.ReadFile(other.path)
			if err != nil {
				t.Fatal(err)
			}
			done := strings.Contains(string(data), "- [x] Shared")
			if done != (j <= i) {
				t.Fatalf("vault %d after write %d: %q", j, i, data)
			}
		}
	}
}

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
	connection.Name = "Renamed vault"
	if _, err := s.ConfigureWorkbench(connection.WorkbenchConnection, common.WorkbenchBridgeInput{}); err != nil {
		t.Fatal(err)
	}
	connection, err = s.WorkbenchSnapshot(connectionID)
	if err != nil || !connection.WriteEnabled || !connection.BridgeConfigured {
		t.Fatalf("business edit changed write authorization: %+v %v", connection, err)
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

func TestWorkbenchBridgeVerificationDoesNotBlockOrOverwriteConcurrentEdit(t *testing.T) {
	vault := t.TempDir()
	started, release := make(chan struct{}), make(chan struct{})
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		path := filepath.Join(vault, strings.TrimPrefix(r.URL.Path, "/vault/"))
		data, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
		w.Header().Set("ETag", "\"proof\"")
		_, _ = w.Write(data)
	}))
	defer bridge.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Original", Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.WorkbenchSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	enabled := true
	verified := make(chan error, 1)
	go func() {
		_, err := s.ConfigureWorkbench(original.WorkbenchConnection, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(certificate), WriteEnabled: &enabled})
		verified <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("bridge verification did not start")
	}
	edited := original.WorkbenchConnection
	edited.Name = "Changed while verifying"
	changed := make(chan error, 1)
	go func() {
		_, err := s.SaveWorkbench(edited)
		changed <- err
	}()
	select {
	case err := <-changed:
		if err != nil {
			close(release)
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("external verification blocked another connection edit")
	}
	close(release)
	if err := <-verified; err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
		t.Fatalf("outdated verification was not rejected: %v", err)
	}
	current, err := s.WorkbenchSnapshot(id)
	if err != nil || current.Name != edited.Name || current.WriteEnabled || current.BridgeConfigured {
		t.Fatalf("outdated verification changed connection: %+v %v", current, err)
	}
}

func TestWorkbenchBridgeVerificationStopsWithService(t *testing.T) {
	vault := t.TempDir()
	started := make(chan struct{})
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer bridge.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	enabled := true
	finished := make(chan error, 1)
	go func() {
		_, err := s.ConfigureWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault}, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(certificate), WriteEnabled: &enabled})
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		_ = s.Close()
		t.Fatal("bridge verification did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("service shutdown did not cancel verification")
	}
	if err := <-finished; err == nil || common.ProtocolError(err).Code != "CONFLICT" {
		t.Fatalf("verification survived service shutdown: %v", err)
	}
}
