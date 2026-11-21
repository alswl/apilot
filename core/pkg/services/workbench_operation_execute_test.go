package services

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func TestExecuteObsidianOperationLostResponse(t *testing.T) {
	vault := t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(note, []byte("# Tasks\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if r.Header.Get("If-Match") != tag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		data, err = io.ReadAll(r.Body)
		if err == nil {
			err = os.WriteFile(path, data, 0600)
		}
		if err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(500)
	}))
	defer bridge.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	enabled := true
	id, err := s.ConfigureWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault}, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(cert), WriteEnabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		snapshot, err := s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.LastSuccess != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scan did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	headings, err := s.WorkbenchHeadings(id, "tasks.md")
	if err != nil || len(headings) != 1 {
		t.Fatalf("headings: %+v %v", headings, err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "lost-response", ConnectionID: id, ConnectionVersion: 1, Title: "Async task", NotePath: "tasks.md", HeadingPath: headings[0].HeadingPath, TargetVersion: headings[0].TargetVersion}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	type result struct {
		receipt common.WorkbenchTaskReceipt
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			receipt, err := s.executeObsidianTaskOperation(context.Background(), input.RequestID)
			results <- result{receipt, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.receipt.SyncState != "confirmed" || second.receipt.SourceObjectID != first.receipt.SourceObjectID {
		t.Fatalf("concurrent replay: %+v %+v", first, second)
	}
	data, err := os.ReadFile(note)
	if err != nil || strings.Count(string(data), "Async task") != 1 {
		t.Fatalf("source: %s %v", data, err)
	}
	headings, err = s.WorkbenchHeadings(id, "tasks.md")
	if err != nil {
		t.Fatal(err)
	}
	input.RequestID, input.Title, input.TargetVersion = "scheduled", "Scheduled task", headings[0].TargetVersion
	accepted, err := s.SubmitWorkbenchTaskOperation(input)
	if err != nil || accepted.SyncState != "accepted" {
		t.Fatalf("submit: %+v %v", accepted, err)
	}
	if _, err := s.SubmitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	scheduled := false
	for time.Now().Before(deadline) {
		receipt, err := s.WorkbenchTaskOperationReceipt(input.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.SyncState == "confirmed" {
			scheduled = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !scheduled {
		t.Fatal("scheduled operation did not complete")
	}
	task, err := s.WorkbenchTask(first.receipt.SourceObjectID)
	if err != nil {
		t.Fatal(err)
	}
	newNote := filepath.Join(vault, "moved.md")
	if err := os.Rename(note, newNote); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, err := s.WorkbenchTask(task.ID)
		if err == nil && current.NativeFields["path"] == "moved.md" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, err := s.WorkbenchTask(task.ID)
	if err != nil || current.NativeFields["path"] != "moved.md" {
		t.Fatalf("move not observed: %+v %v", current, err)
	}
	movedReceipt, err := s.WriteWorkbenchTask(common.WorkbenchTaskWrite{RequestID: "moved-finish", ConnectionID: id, ConnectionVersion: 1, SourceObjectID: task.ID, TargetVersion: task.TargetVersion, State: "done"})
	if err != nil || movedReceipt.SyncState != "confirmed" {
		t.Fatalf("moved operation: %+v %v", movedReceipt, err)
	}
	data, err = os.ReadFile(newNote)
	if err != nil || !strings.Contains(string(data), "- [x] Async task") {
		t.Fatalf("moved source: %s %v", data, err)
	}
}

func TestExecuteAlreadySatisfiedOperationDoesNotWrite(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	source := []byte("- [x] Finished 🆔 stable ✅ 2026-11-20\n")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("read: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "already-done", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[0].ID, TargetVersion: "previous-version", State: "done"}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeObsidianTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "confirmed" {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(source) {
		t.Fatalf("source changed: %s %v", actual, err)
	}
}

func TestOperationGateSharedAcrossVaultAliases(t *testing.T) {
	parent := t.TempDir()
	vault := filepath.Join(parent, "vault")
	alias := filepath.Join(parent, "alias")
	if err := os.Mkdir(vault, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vault, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "tasks.md"), []byte("# Tasks\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct{ id, path string }{{"first", vault}, {"second", alias}} {
		conn := common.WorkbenchConnection{ID: pair.id, Name: pair.id, Kind: common.WorkbenchObsidianTasks, Vault: pair.path, Scope: ".", Version: 1, WriteEnabled: true}
		if err := store.SaveWorkbench(conn, false); err != nil {
			t.Fatal(err)
		}
		input := common.WorkbenchTaskWrite{RequestID: pair.id, ConnectionID: pair.id, ConnectionVersion: 1, Title: "Task", NotePath: "tasks.md"}
		if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
			t.Fatal(err)
		}
	}
	canonical, err := filepath.EvalSymlinks(vault)
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.operationNotes.lock(context.Background(), filepath.Join(canonical, "tasks.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, id := range []string{"first", "second"} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := s.executeObsidianTaskOperation(ctx, id)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s bypassed shared note gate: %v", id, err)
		}
		op, err := s.store.(workbenchTaskOperationStore).WorkbenchTaskOperation(id)
		if err != nil || op.Phase != "accepted" {
			t.Fatalf("%s changed while waiting: %+v %v", id, op, err)
		}
	}
}

func TestExecuteStaleCreationTargetIsConflict(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	source := []byte("# Tasks\nExisting content\n")
	if err := os.WriteFile(path, source, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "stale", ConnectionID: conn.ID, ConnectionVersion: 1, Title: "Task", NotePath: "tasks.md", HeadingPath: []string{"Tasks"}, TargetVersion: "stale-version"}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeObsidianTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "conflict" {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(source) {
		t.Fatalf("source changed: %s %v", actual, err)
	}
}

func TestExecuteMissingTaskReportsConflict(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: t.TempDir(), Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	task := common.WorkbenchTask{ID: "task", ConnectionID: conn.ID, SourceKey: "key", Title: "Task", State: "open", NativeFields: map[string]string{"path": "task.md"}}
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "missing", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: task.ID, State: "done", TargetVersion: workbenchFingerprint(task)}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordWorkbench(conn, nil, []common.WorkbenchTask{task}, nil, &now); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeObsidianTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "conflict" {
		t.Fatalf("missing task: %+v %v", receipt, err)
	}
}

func TestQueuedOperationDoesNotDispatchToOldNote(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	oldPath := filepath.Join(vault, "old.md")
	if err := os.WriteFile(oldPath, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "move", Name: "Move", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "move-queued", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchTaskTargetVersion(tasks[0])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(vault)
	if err != nil {
		t.Fatal(err)
	}
	release, err := s.operationNotes.lock(context.Background(), filepath.Join(canonical, "old.md"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.executeObsidianTaskOperation(context.Background(), input.RequestID); done <- err }()
	time.Sleep(50 * time.Millisecond)
	changed := tasks[0]
	changed.NativeFields = map[string]string{}
	for k, v := range tasks[0].NativeFields {
		changed.NativeFields[k] = v
	}
	changed.NativeFields["path"] = "new.md"
	if !workbenchTaskMatchesTarget(changed, input.TargetVersion) {
		t.Fatal("a pure move invalidated the task precondition")
	}
	if err := store.RecordWorkbench(conn, nil, tasks, []common.WorkbenchTask{changed}, &now); err != nil {
		t.Fatal(err)
	}
	release()
	select {
	case err := <-done:
		if err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
			t.Fatalf("expected stale locator, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("queued operation did not return")
	}
	op, err := s.store.(workbenchTaskOperationStore).WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "accepted" {
		t.Fatalf("operation dispatched: %+v %v", op, err)
	}
}

func TestLocationOperationRejectsSnapshotChangeBeforeDispatch(t *testing.T) {
	vault := t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	original := []byte("- [ ] Existing\n")
	if err := os.WriteFile(note, original, 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	signal := filepath.Join(bin, "reading")
	release := filepath.Join(bin, "release")
	script := "#!/bin/sh\n: > \"$APILOT_TEST_READING\"\nwhile [ ! -f \"$APILOT_TEST_RELEASE\" ]; do sleep 0.01; done\nprintf '%s\\n' \"$APILOT_TEST_VAULT\"\n"
	if err := os.WriteFile(filepath.Join(bin, "obsidian"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APILOT_TEST_VAULT", vault)
	t.Setenv("APILOT_TEST_READING", signal)
	t.Setenv("APILOT_TEST_RELEASE", release)
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "location-fence", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("initial tasks: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	now := time.Now().UTC()
	recordObsidianImport(conn, nil, tasks, now)
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
		t.Fatal(err)
	}
	persisted, err := store.WorkbenchObject(tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "location-snapshot-fence", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: persisted.ID, State: "done", TargetVersion: workbenchTaskTargetVersion(persisted)}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		receipt common.WorkbenchTaskReceipt
		err     error
	}
	done := make(chan result, 1)
	go func() {
		receipt, err := s.executeObsidianTaskOperation(ctx, input.RequestID)
		done <- result{receipt, err}
	}()
	for {
		if _, err := os.Stat(signal); err == nil {
			break
		}
		select {
		case result := <-done:
			t.Fatalf("operation stopped before vault verification: %+v %v", result.receipt, result.err)
		default:
		}
		if ctx.Err() != nil {
			t.Fatal("CLI did not reach vault verification")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := store.RecordWorkbench(conn, nil, tasks, tasks, &now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if result := <-done; result.err == nil || common.ProtocolError(result.err).Code != "STALE_REVISION" {
		t.Fatalf("expected stale snapshot, got %+v %v", result.receipt, result.err)
	}
	op, err := s.store.(workbenchTaskOperationStore).WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "accepted" {
		t.Fatalf("operation dispatched: %+v %v", op, err)
	}
	data, err := os.ReadFile(note)
	if err != nil || string(data) != string(original) {
		t.Fatalf("source changed: %q %v", data, err)
	}
}

func TestBridgeOperationRejectsChangedMappingBeforeDispatch(t *testing.T) {
	vault := t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	original := []byte("- [ ] Existing\n")
	if err := os.WriteFile(note, original, 0600); err != nil {
		t.Fatal(err)
	}
	reading, resume := make(chan struct{}), make(chan struct{})
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/vault/")
		if path == "tasks.md" {
			close(reading)
			<-resume
		}
		data, err := os.ReadFile(filepath.Join(vault, path))
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
		w.Header().Set("ETag", `"revision"`)
		_, _ = w.Write(data)
	}))
	defer bridge.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn := common.WorkbenchConnection{ID: "bridge-fence", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true, BridgeConfigured: true}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	settings := common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(cert)}
	if err := s.store.(workbenchBridgeStore).SaveWorkbenchBridge(conn, false, &settings); err != nil {
		t.Fatal(err)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("initial tasks: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	now := time.Now().UTC()
	recordObsidianImport(conn, nil, tasks, now)
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
		t.Fatal(err)
	}
	task, err := store.WorkbenchObject(tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "bridge-mapping-fence", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: task.ID, State: "done", TargetVersion: workbenchTaskTargetVersion(task)}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.executeObsidianTaskOperation(ctx, input.RequestID); done <- err }()
	select {
	case <-reading:
	case err := <-done:
		t.Fatalf("operation stopped before target read: %v", err)
	case <-ctx.Done():
		t.Fatal("bridge did not reach target read")
	}
	changed := task
	changed.Title = "A different saved mapping"
	if err := store.RecordWorkbench(conn, nil, tasks, []common.WorkbenchTask{changed}, &now); err != nil {
		close(resume)
		t.Fatal(err)
	}
	close(resume)
	if err := <-done; err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
		t.Fatalf("expected stale mapping, got %v", err)
	}
	op, err := s.store.(workbenchTaskOperationStore).WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "accepted" {
		t.Fatalf("operation dispatched: %+v %v", op, err)
	}
	data, err := os.ReadFile(note)
	if err != nil || string(data) != string(original) {
		t.Fatalf("source changed: %q %v", data, err)
	}
}

func TestBridgeCreationRejectsMarkerPublishedBeforeDispatch(t *testing.T) {
	vault := t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	original := []byte("# Tasks\n")
	if err := os.WriteFile(note, original, 0600); err != nil {
		t.Fatal(err)
	}
	reading, resume := make(chan struct{}), make(chan struct{})
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/vault/")
		if path == "tasks.md" {
			close(reading)
			<-resume
		}
		data, err := os.ReadFile(filepath.Join(vault, path))
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
		w.Header().Set("ETag", `"revision"`)
		_, _ = w.Write(data)
	}))
	defer bridge.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn := common.WorkbenchConnection{ID: "bridge-creation-fence", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true, BridgeConfigured: true}
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	settings := common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(cert)}
	if err := s.store.(workbenchBridgeStore).SaveWorkbenchBridge(conn, false, &settings); err != nil {
		t.Fatal(err)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	headings, err := s.WorkbenchHeadings(conn.ID, "tasks.md")
	if err != nil || len(headings) != 1 {
		t.Fatalf("headings: %+v %v", headings, err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "bridge-marker-fence", ConnectionID: conn.ID, ConnectionVersion: 1, NotePath: "tasks.md", HeadingPath: headings[0].HeadingPath, TargetVersion: headings[0].TargetVersion, Title: "New"}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	op, err := ops.WorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.executeObsidianTaskOperation(ctx, input.RequestID); done <- err }()
	select {
	case <-reading:
	case err := <-done:
		t.Fatalf("operation stopped before target read: %v", err)
	case <-ctx.Done():
		t.Fatal("bridge did not reach target read")
	}
	duplicate := "- [ ] Duplicate 🆔 " + obsidianCreationMarker(op.Fingerprint) + "\n"
	if err := os.WriteFile(filepath.Join(vault, "other.md"), []byte(duplicate), 0600); err != nil {
		close(resume)
		t.Fatal(err)
	}
	tasks, err := readWorkbenchObsidianWrittenVault(ctx, conn)
	if err != nil {
		close(resume)
		t.Fatal(err)
	}
	matchWorkbench(conn, nil, tasks)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
		close(resume)
		t.Fatal(err)
	}
	close(resume)
	if err := <-done; err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
		t.Fatalf("expected stale mapping, got %v", err)
	}
	op, err = ops.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "accepted" {
		t.Fatalf("operation dispatched: %+v %v", op, err)
	}
	data, err := os.ReadFile(note)
	if err != nil || string(data) != string(original) {
		t.Fatalf("target note changed: %q %v", data, err)
	}
}

func TestPrepareStateRejectsConcurrentSourceCheckboxChange(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "task.md")
	if err := os.WriteFile(path, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1}
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("initial: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	input := common.WorkbenchTaskWrite{RequestID: "state", ConnectionVersion: 1, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchTaskTargetVersion(tasks[0])}
	changed := []byte("- [-] Task 🆔 stable\n")
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	_, _, _, receipt, err := prepareWorkbenchObsidianState(context.Background(), common.WorkbenchSnapshot{WorkbenchConnection: conn}, input, tasks[0])
	if err != nil || receipt.SyncState != "conflict" {
		t.Fatalf("expected conflict: %+v %v", receipt, err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != string(changed) {
		t.Fatalf("source overwritten: %s %v", actual, err)
	}
}

func TestWorkbenchNoteGatesReleaseIdleEntries(t *testing.T) {
	var gates workbenchNoteGates
	release, err := gates.lock(context.Background(), "note")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelled := make(chan error, 1)
	go func() {
		_, err := gates.lock(ctx, "note")
		cancelled <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		gates.mu.Lock()
		users := gates.items["note"].users
		gates.mu.Unlock()
		if users == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiter did not register")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter: %v", err)
	}
	gates.mu.Lock()
	users := gates.items["note"].users
	gates.mu.Unlock()
	if users != 1 {
		t.Fatalf("canceled waiter retained: %d", users)
	}
	release()
	gates.mu.Lock()
	remaining := len(gates.items)
	gates.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("idle gates retained: %d", remaining)
	}
}

func TestPrepareNoNativeIDIgnoresUnrelatedUnreadableNote(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "target.md")
	if err := os.WriteFile(path, []byte("# Tasks\n- [ ] Review\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "unrelated.md"), []byte("- [ ] "+strings.Repeat("x", 13<<10)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1}
	tasks, err := integrations.ReadWorkbenchObsidianTasksInNote(context.Background(), conn, "target.md")
	if err != nil || len(tasks) != 1 || tasks[0].NativeID != "" {
		t.Fatalf("target: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	input := common.WorkbenchTaskWrite{RequestID: "single-note", ConnectionID: conn.ID, ConnectionVersion: conn.Version, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchTaskTargetVersion(tasks[0])}
	_, _, replacement, conflict, err := prepareWorkbenchObsidianState(context.Background(), common.WorkbenchSnapshot{WorkbenchConnection: conn}, input, tasks[0])
	if err != nil || conflict.SyncState != "" || !strings.Contains(string(replacement), "- [x] Review") {
		t.Fatalf("single note preparation: %q %+v %v", replacement, conflict, err)
	}
	if _, err := readWorkbenchObsidianWrittenVault(context.Background(), conn); err == nil {
		t.Fatal("fixture did not make the unrelated full read fail")
	}
}

func TestNoNativeIDWriteConfirmsWhenAnotherNoteIsUnreadable(t *testing.T) {
	vault := t.TempDir()
	note := filepath.Join(vault, "target.md")
	other := filepath.Join(vault, "other.md")
	if err := os.WriteFile(note, []byte("# Tasks\n- [ ] Review\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("- [ ] Other 🆔 other\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(vault, strings.TrimPrefix(r.URL.Path, "/vault/"))
		data, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		etag := "\"" + workbenchFingerprint(string(data)) + "\""
		if r.Method == http.MethodGet {
			w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
			w.Header().Set("ETag", etag)
			_, _ = w.Write(data)
			return
		}
		if r.Header.Get("If-Match") != etag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		data, err = io.ReadAll(r.Body)
		if err != nil || os.WriteFile(path, data, 0600) != nil {
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
	id, err := s.ConfigureWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault}, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(cert), WriteEnabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var target common.WorkbenchTask
	for time.Now().Before(deadline) {
		page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range page.Items {
			if task.Title == "Review" {
				target = task
			}
		}
		if target.ID != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if target.ID == "" || target.NativeID != "" {
		t.Fatalf("target unavailable: %+v", target)
	}
	if err := os.WriteFile(other, []byte("- [ ] "+strings.Repeat("x", 13<<10)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	var before common.WorkbenchSnapshot
	for time.Now().Before(deadline) {
		before, err = s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(before.Attempts) > 0 && before.Attempts[0].State == "failed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before.LastSuccess == nil || len(before.Attempts) == 0 || before.Attempts[0].State != "failed" {
		t.Fatalf("fixture did not fail full scan: %+v", before)
	}
	input := common.WorkbenchTaskWrite{RequestID: "no-id", ConnectionID: id, ConnectionVersion: target.ConnectionVersion, SourceObjectID: target.ID, State: "done", TargetVersion: target.TargetVersion}
	receipt, err := s.WriteWorkbenchTask(input)
	if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID != target.ID {
		t.Fatalf("operation: %+v %v", receipt, err)
	}
	after, err := s.WorkbenchSnapshot(id)
	if err != nil || after.LastSuccess == nil || !after.LastSuccess.Equal(*before.LastSuccess) {
		t.Fatalf("single note changed full confirmation time: %+v %v", after, err)
	}
	confirmed, err := s.WorkbenchTask(target.ID)
	if err != nil || confirmed.State != "done" {
		t.Fatalf("task was not confirmed: %+v %v", confirmed, err)
	}
}

func TestCreateRejectsMarkerAlreadyPresentInAnotherNote(t *testing.T) {
	vault := t.TempDir()
	target := filepath.Join(vault, "target.md")
	content := []byte("# Tasks\n")
	if err := os.WriteFile(target, content, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	headings := integrations.WorkbenchObsidianHeadings(content)
	input := common.WorkbenchTaskWrite{RequestID: "new-task", ConnectionID: conn.ID, ConnectionVersion: 1, NotePath: "target.md", HeadingPath: headings[0].HeadingPath, TargetVersion: headings[0].TargetVersion, Title: "Create"}
	marker := obsidianCreationMarker(workbenchFingerprint(input))
	if err := os.WriteFile(filepath.Join(vault, "other.md"), []byte("- [ ] Existing 🆔 "+marker+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeObsidianTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "conflict" {
		t.Fatalf("duplicate creation marker: %+v %v", receipt, err)
	}
	after, err := os.ReadFile(target)
	if err != nil || string(after) != string(content) {
		t.Fatalf("conflicting creation modified source: %q %v", after, err)
	}
}

func TestNativeIDPreparationUsesProvenCoverage(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "target.md"), []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "other.md"), []byte("- [ ] "+strings.Repeat("x", 13<<10)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1}
	tasks, err := integrations.ReadWorkbenchObsidianTasksInNote(context.Background(), conn, "target.md")
	if err != nil || len(tasks) != 1 {
		t.Fatalf("target: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	input := common.WorkbenchTaskWrite{RequestID: "native", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchTaskTargetVersion(tasks[0])}
	_, _, replacement, conflict, err := prepareWorkbenchObsidianStateWithCoverage(context.Background(), common.WorkbenchSnapshot{WorkbenchConnection: conn}, input, tasks[0], true)
	if err != nil || conflict.SyncState != "" || !strings.Contains(string(replacement), "- [x] Task") {
		t.Fatalf("covered target: %q %+v %v", replacement, conflict, err)
	}
	if _, _, _, _, err := prepareWorkbenchObsidianStateWithCoverage(context.Background(), common.WorkbenchSnapshot{WorkbenchConnection: conn}, input, tasks[0], false); err == nil {
		t.Fatal("uncovered identity should require a full read")
	}
}
