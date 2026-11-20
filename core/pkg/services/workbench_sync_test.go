package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchSyncSupersededAndIsolated(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	s.mu.Lock()
	s.workbenchReader = func(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		if c.Scope == "Old" {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return []common.WorkbenchTask{{SourceKey: "obsolete", Title: "Obsolete", State: "open"}}, nil
		}
		if c.Scope == "Fail" {
			return nil, errors.New("partial read")
		}
		return []common.WorkbenchTask{{SourceKey: "current", Title: "Current", State: "open"}}, nil
	}
	s.mu.Unlock()
	config := common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: t.TempDir(), Scope: "Old"}
	id, err := s.SaveWorkbench(config)
	if err != nil {
		t.Fatal(err)
	}
	await := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for sync")
		}
	}
	await(started)
	if err := s.SyncWorkbench(id); err != nil {
		t.Fatal(err)
	}
	first, err := s.WorkbenchSnapshot(id)
	if err != nil || len(first.Attempts) != 1 {
		t.Fatalf("%+v %v", first, err)
	}
	other, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchGitHubIssues, Name: "Independent", Repository: "alswl/issues"})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(id, state string) common.WorkbenchSnapshot {
		t.Helper()
		for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
			c, err := s.WorkbenchSnapshot(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(c.Attempts) > 0 && c.Attempts[0].State == state {
				return c
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("attempt for %s did not reach %s", id, state)
		return common.WorkbenchSnapshot{}
	}
	wait(other, "success")
	config = first.WorkbenchConnection
	config.Scope = "New"
	if _, err := s.SaveWorkbench(config); err != nil {
		t.Fatal(err)
	}
	await(cancelled)
	current := wait(id, "success")
	if current.Version != 2 || len(current.Attempts) < 2 || current.Attempts[1].State != "superseded" {
		t.Fatalf("%+v", current)
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Current" {
		t.Fatalf("%+v %v", page, err)
	}
	s.mu.Lock()
	s.workbenchReader = func(context.Context, common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		return []common.WorkbenchTask{{SourceKey: "partial", Title: "Partial"}}, errors.New("partial read")
	}
	s.mu.Unlock()
	if err := s.SyncWorkbench(id); err != nil {
		t.Fatal(err)
	}
	failed := wait(id, "failed")
	page, err = s.WorkbenchTasks(common.WorkbenchQuery{Connection: id, Revision: page.Revision})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Current" || !failed.LastSuccess.Equal(*current.LastSuccess) {
		t.Fatalf("%+v %+v %v", failed, page, err)
	}
}

func TestWorkbenchPauseCancelAndResume(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.mu.Lock()
	s.workbenchReader = func(ctx context.Context, _ common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		<-ctx.Done()
		return []common.WorkbenchTask{{SourceKey: "partial", Title: "Do not commit", State: "open"}}, nil
	}
	s.mu.Unlock()
	id, err := s.SaveWorkbench(common.WorkbenchConnection{
		Kind: common.WorkbenchObsidianTasks, Name: "Test vault", Vault: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.WorkbenchSnapshot(id)
	if err != nil || len(first.Attempts) != 1 {
		t.Fatalf("initial attempt: %+v %v", first, err)
	}
	firstID := first.Attempts[0].ID
	if _, err := s.CancelWorkbenchSync(id, firstID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		current, err := s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		if current.Attempts[0].State == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sync did not cancel")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := s.CancelWorkbenchSync(id, firstID); err != nil {
		t.Fatalf("repeated cancellation: %v", err)
	}
	paused, err := s.PauseWorkbench(id, first.Version, true)
	if err != nil || !paused.Paused {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	if err := s.SyncWorkbench(id); err == nil {
		t.Fatal("paused connection accepted a refresh")
	}
	resumed, err := s.PauseWorkbench(id, paused.Version, false)
	if err != nil || resumed.Paused || resumed.Attempts[0].ID == firstID {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
	latest, err := s.CancelWorkbenchSync(id, firstID)
	if err != nil || latest.Attempts[0].State != "running" {
		t.Fatalf("old cancel affected current attempt: %+v %v", latest, err)
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("cancelled scan committed tasks: %+v %v", page, err)
	}
}

func TestLocalWorkbenchPluginPersistsDisabledState(t *testing.T) {
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	batch := common.WorkbenchImport{Tasks: []common.WorkbenchImportTask{{Key: "keep", Title: "Keep me", State: "open"}}}
	if _, err := s.ImportWorkbench(batch); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if _, err := s.SetWorkbenchPluginEnabled(common.WorkbenchLocal, false); err != nil {
		s.Close()
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.ImportWorkbench(common.WorkbenchImport{Tasks: []common.WorkbenchImportTask{}}); err == nil {
		t.Fatal("disabled Local accepted an import")
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: common.DefaultWorkbenchLocalID})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("Local tasks: %+v %v", page, err)
	}
	if _, err := s.SetWorkbenchPluginEnabled(common.WorkbenchLocal, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportWorkbench(batch); err != nil {
		t.Fatal(err)
	}
	after, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: common.DefaultWorkbenchLocalID})
	if err != nil || len(after.Items) != 1 || after.Items[0].ID != page.Items[0].ID {
		t.Fatalf("Local task identity changed: %+v %v", after, err)
	}
}

func TestLocalWorkbenchSingleWriteReplayAndStaleState(t *testing.T) {
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	input := common.WorkbenchTaskWrite{
		RequestID: "create-once", ConnectionID: common.DefaultWorkbenchLocalID, ConnectionVersion: 1, Title: "Local task",
	}
	created, err := s.WriteWorkbenchTask(input)
	if err != nil || created.SyncState != "confirmed" {
		t.Fatalf("create: %+v %v", created, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.WriteWorkbenchTask(input)
	if err != nil || replayed != created {
		t.Fatalf("replay: %+v %v", replayed, err)
	}
	task, err := s.WorkbenchTask(created.SourceObjectID)
	if err != nil || task.ID != created.SourceObjectID || task.Title != input.Title || task.State != "open" {
		t.Fatalf("read back created Local task after restart: task=%+v error=%v", task, err)
	}
	state := common.WorkbenchTaskWrite{
		RequestID: "complete", SourceObjectID: task.ID, ConnectionVersion: task.ConnectionVersion, TargetVersion: task.TargetVersion, State: "done",
	}
	completed, err := s.WriteWorkbenchTask(state)
	if err != nil || completed.SyncState != "confirmed" {
		t.Fatalf("complete: %+v %v", completed, err)
	}
	state.RequestID, state.State = "stale-reopen", "open"
	stale, err := s.WriteWorkbenchTask(state)
	if err != nil || stale.SyncState != "conflict" {
		t.Fatalf("stale reopen: %+v %v", stale, err)
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: input.ConnectionID})
	if err != nil || len(page.Items) != 1 || page.Items[0].State != "done" {
		t.Fatalf("Local tasks: %+v %v", page, err)
	}
}

func TestWorkbenchCapabilitiesFollowConnectionPolicy(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	local, err := s.WorkbenchSnapshot(common.DefaultWorkbenchLocalID)
	if err != nil || !local.Capabilities.Create || !local.Capabilities.SetState || !local.Capabilities.Import || local.Capabilities.Refresh {
		t.Fatalf("Local capabilities: %+v %v", local.Capabilities, err)
	}
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Read-only vault", Vault: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	vault, err := s.WorkbenchSnapshot(id)
	if err != nil || !vault.Capabilities.Refresh || !vault.Capabilities.ListTargets || vault.Capabilities.Create || vault.Capabilities.SetState {
		t.Fatalf("Obsidian capabilities: %+v %v", vault.Capabilities, err)
	}
	paused, err := s.PauseWorkbench(id, vault.Version, true)
	if err != nil || paused.Capabilities != (common.WorkbenchCapabilities{}) {
		t.Fatalf("paused capabilities: %+v %v", paused.Capabilities, err)
	}
	if _, err := s.SetWorkbenchPluginEnabled(common.WorkbenchLocal, false); err != nil {
		t.Fatal(err)
	}
	local, err = s.WorkbenchSnapshot(common.DefaultWorkbenchLocalID)
	if err != nil || !local.PluginDisabled || local.Capabilities.Create {
		t.Fatalf("disabled Local: %+v %v", local, err)
	}
}

func TestWorkbenchObsidianSyncRecordsSafeReadFailure(t *testing.T) {
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "Oversize.md"), make([]byte, (8<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.SaveWorkbench(common.WorkbenchConnection{
		Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault, Scope: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		snapshot, err := s.WorkbenchSyncDetails(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Attempts) == 0 || snapshot.Attempts[0].State == "running" {
			continue
		}
		attempt := snapshot.Attempts[0]
		if attempt.State != "failed" || attempt.Error != `Read Markdown note "Oversize.md": note exceeds 8 MiB` {
			t.Fatalf("unexpected sync diagnostic: %+v", attempt)
		}
		return
	}
	t.Fatal("sync did not finish")
}

func TestWorkbenchReadDeadlineFailsWithoutPublishingPartialTasks(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	connection := common.WorkbenchConnection{
		ID: "deadline-test", Kind: common.WorkbenchObsidianTasks, Mode: "local", Version: 1,
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWorkbench(connection, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	attempts := []common.WorkbenchAttempt{{ID: now.UnixNano(), State: "running", Started: now}}
	if err := store.RecordWorkbench(connection, attempts, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	s.wg.Add(1)
	s.runWorkbench(ctx, cancel, store, connection, attempts, func(context.Context, common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		return []common.WorkbenchTask{{SourceKey: "partial", Title: "Partial", State: "open"}}, context.DeadlineExceeded
	})
	snapshot, err := s.WorkbenchSnapshot(connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if attempt := snapshot.Attempts[0]; attempt.State != "failed" || attempt.Error != "Source read timed out after 30s" {
		t.Fatalf("deadline attempt: %+v", attempt)
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: connection.ID})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("deadline published a partial snapshot: %+v %v", page, err)
	}
}
