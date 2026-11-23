package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/dal"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func TestReconcileOperationConfirmsOnlyObservedSource(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("- [ ] Task 🆔 unique\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
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
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchFingerprint(tasks[0])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := claimed.Receipt
	receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", receipt); err != nil {
		t.Fatal(err)
	}
	got, err := s.reconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
	if err != nil || got.SyncState != "needs_review" {
		t.Fatalf("unmet target: %+v %v", got, err)
	}
	if err := os.WriteFile(path, []byte("- [x] Task 🆔 unique ✅ 2026-11-20\n- [x] Other 🆔 unique ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = s.reconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
	if err != nil || got.SyncState != "needs_review" {
		t.Fatalf("duplicate identity must remain unconfirmed: %+v %v", got, err)
	}
	if err := os.WriteFile(path, []byte("- [x] Task 🆔 unique ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = s.reconcileWorkbenchTaskOperationWithReader(context.Background(), input.RequestID, func(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		observed, err := readWorkbenchObsidianWrittenVault(ctx, c)
		if err != nil {
			return nil, err
		}
		// A newer scan commits after this observation began.
		if err := store.RecordWorkbench(conn, nil, tasks, tasks, &now); err != nil {
			return nil, err
		}
		return observed, nil
	})
	if err == nil || got.SyncState != "needs_review" {
		t.Fatalf("stale observation must not confirm: %+v %v", got, err)
	}
	operation, err := ops.WorkbenchTaskOperation(input.RequestID)
	if err != nil || operation.Phase != "needs_review" {
		t.Fatalf("stale observation changed operation: %+v %v", operation, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := time.Now().Add(5 * time.Second)
	confirmed := false
	for time.Now().Before(deadline) {
		op, err := s.store.(workbenchTaskOperationStore).WorkbenchTaskOperation(input.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		if op.Receipt.SyncState == "confirmed" && op.Receipt.SourceObjectID == tasks[0].ID {
			confirmed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !confirmed {
		t.Fatal("restart did not confirm the observed source")
	}
	if err := os.WriteFile(path, []byte("- [ ] Task 🆔 unique\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncWorkbench(conn.ID); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.WorkbenchTask(tasks[0].ID)
		if err == nil && task.State == "open" {
			receipt, err := s.WorkbenchTaskOperationReceipt(input.RequestID)
			if err != nil || receipt.SyncState != "confirmed" {
				t.Fatalf("historical receipt changed: %+v %v", receipt, err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("source reopen did not update snapshot")
}

func TestReconcileOperationWithoutNativeIDUsesHeading(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("# First\n- [ ] Review\n# Second\n- [ ] Review\n"), 0600); err != nil {
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
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "review", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[1].ID, State: "done", TargetVersion: workbenchFingerprint(tasks[1])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := claimed.Receipt
	receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# First\n- [ ] Review\n# Second\n- [x] Review ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	type result struct {
		receipt common.WorkbenchTaskReceipt
		err     error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			receipt, err := s.ReconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
			results <- result{receipt, err}
		}()
	}
	for range 2 {
		got := <-results
		if got.err != nil || got.receipt.SyncState != "confirmed" || got.receipt.SourceObjectID != tasks[1].ID {
			t.Fatalf("confirmation: %+v %v", got.receipt, got.err)
		}
	}
}

func TestReconcileOperationWithUnrelatedDuplicateIdentity(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("- [ ] First 🆔 a\n- [ ] Target 🆔 b\n"), 0600); err != nil {
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
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("initial tasks: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	confirmedAt := time.Now().UTC().Add(-time.Minute)
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &confirmedAt); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[1].ID, State: "done", TargetVersion: workbenchFingerprint(tasks[1])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("- [x] Changed 🆔 a\n- [ ] Duplicate 🆔 a\n- [x] Target 🆔 b ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.reconcileWorkbenchOperationWithRetry(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID != tasks[1].ID {
		t.Fatalf("unrelated duplicate prevented confirmation: %+v %v", receipt, err)
	}
	first, err := store.WorkbenchObject(tasks[0].ID)
	if err != nil || first.Title != "First" || first.State != "open" {
		t.Fatalf("ambiguous task changed: %+v %v", first, err)
	}
	target, err := store.WorkbenchObject(tasks[1].ID)
	if err != nil || target.State != "done" {
		t.Fatalf("target was not confirmed: %+v %v", target, err)
	}
	snapshot, err := s.WorkbenchSnapshot(conn.ID)
	if err != nil || snapshot.LastSuccess == nil || !snapshot.LastSuccess.Equal(confirmedAt) {
		t.Fatalf("partial verification advanced full confirmation: %+v %v", snapshot, err)
	}
}

func TestLegacyOperationReplayRestoresRequestForSafeReconciliation(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
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
	input := common.WorkbenchTaskWrite{RequestID: "legacy", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchFingerprint(tasks[0])}
	writer := s.store.(workbenchTaskWriteStore)
	prior := common.WorkbenchTaskReceipt{RequestID: input.RequestID, ConnectionID: conn.ID, SourceObjectID: tasks[0].ID, SyncState: "needs_review"}
	if err := writer.StageWorkbenchTaskWrite(workbenchFingerprint(input), prior); err != nil {
		t.Fatal(err)
	}
	source := []byte("- [x] Task 🆔 stable ✅ 2026-11-20\n")
	if err := os.WriteFile(path, source, 0600); err != nil {
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
	wrong := input
	wrong.State = "open"
	if _, err := s.WriteWorkbenchTask(wrong); err == nil {
		t.Fatal("accepted a different replayed request after restart")
	}
	receipt, err := s.WriteWorkbenchTask(input)
	if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID != tasks[0].ID {
		t.Fatalf("legacy reconciliation: %+v %v", receipt, err)
	}
	op, err := s.store.(workbenchTaskOperationStore).WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "confirmed" || op.Input.RequestID != input.RequestID {
		t.Fatalf("request was not retained: %+v %v", op, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(source) {
		t.Fatalf("reconciliation rewrote source: %q %v", after, err)
	}
}

func TestOperationReadbackDoesNotRemoveUnrelatedUnverifiedTask(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("- [ ] Other 🆔 other\n- [ ] Target 🆔 target\n"), 0600); err != nil {
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
	before, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(before) != 2 {
		t.Fatalf("tasks: %+v %v", before, err)
	}
	matchWorkbench(conn, nil, before)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, before, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "target-only", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: before[1].ID, State: "done", TargetVersion: workbenchFingerprint(before[1])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("- [ ] Other 🆔 other\n- [x] Target 🆔 target ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.reconcileWorkbenchTaskOperationWithReader(context.Background(), input.RequestID, func(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		tasks, err := readWorkbenchObsidianWrittenVault(ctx, c)
		if err != nil {
			return nil, err
		}
		return tasks[1:], nil
	})
	if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID != before[1].ID {
		t.Fatalf("target confirmation: %+v %v", receipt, err)
	}
	other, err := store.WorkbenchObject(before[0].ID)
	if err != nil || other.Title != "Other" {
		t.Fatalf("unrelated task removed from a single observation: %+v %v", other, err)
	}
}

func TestReconcileOperationAfterPermissionChange(t *testing.T) {
	for _, change := range []string{"pause", "revoke", "scope"} {
		t.Run(change, func(t *testing.T) {
			vault := t.TempDir()
			path := filepath.Join(vault, "tasks.md")
			if err := os.WriteFile(path, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(vault, "other"), 0700); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
			tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("tasks: %+v %v", tasks, err)
			}
			matchWorkbench(conn, nil, tasks)
			// Seed before starting the service so its watcher cannot import the same task concurrently.
			func() {
				store, err := dal.Open(filepath.Join(dir, "apilot.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if err := store.Close(); err != nil {
						t.Error(err)
					}
				}()
				if err := store.SaveWorkbench(conn, false); err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
					t.Fatal(err)
				}
			}()
			open := func() (*Service, error) {
				return NewWithWorkbenchDependencies(dir, func(path string) (Store, error) { return dal.Open(path) },
					BuiltinWorkbenchPluginRegistry(), ObsidianSyncOptions{MaxRetries: 3},
					WorkbenchGitHubDependencies{Credentials: githubTestCredentials{}})
			}
			s, err := open()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = s.Close() }()
			input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchFingerprint(tasks[0])}
			if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
				t.Fatal(err)
			}
			ops := s.store.(workbenchTaskOperationStore)
			claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			claimed.Receipt.SyncState = "needs_review"
			if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("- [x] Task 🆔 stable ✅ 2026-11-20\n"), 0600); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "pause":
				if _, err := s.PauseWorkbench(conn.ID, conn.Version, true); err != nil {
					t.Fatal(err)
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				s, err = open()
				if err != nil {
					t.Fatal(err)
				}
			case "revoke", "scope":
				enabled := false
				if change == "scope" {
					conn.Scope = "other"
				}
				if _, err := s.ConfigureWorkbench(conn, common.WorkbenchBridgeInput{WriteEnabled: &enabled}); err != nil {
					t.Fatal(err)
				}
			}
			receipt, err := s.ReconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			if change == "scope" {
				if receipt.SyncState != "needs_review" {
					t.Fatalf("changed source scope was confirmed: %+v", receipt)
				}
			} else if receipt.SyncState != "confirmed" || receipt.SourceObjectID != tasks[0].ID {
				t.Fatalf("permission change prevented source verification: %+v", receipt)
			}
		})
	}
}

func TestLocationBoundReconcileRejectsPreviouslyDuplicatedTask(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("# Tasks\n- [ ] Review\n- [ ] Review\n"), 0600); err != nil {
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
	tasks, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks: %+v %v", tasks, err)
	}
	matchWorkbench(conn, nil, tasks)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, tasks, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "ambiguous-location", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: tasks[0].ID, State: "done", TargetVersion: workbenchFingerprint(tasks[0])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Tasks\n- [x] Review ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.ReconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "needs_review" {
		t.Fatalf("ambiguous old mapping confirmed: %+v %v", receipt, err)
	}
}

func TestNativeIDReadbackUsesVerifiedWatchedCoverage(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
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
	if err := s.SyncWorkbench(conn.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		ready := s.workbenchIdentityCoverageLocked(conn, "tasks.md") && s.workbenchActive[conn.ID] == nil
		s.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watched full coverage not established")
		}
		time.Sleep(10 * time.Millisecond)
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("task: %+v %v", page, err)
	}
	task := page.Items[0]
	before, err := s.WorkbenchSnapshot(conn.ID)
	if err != nil || before.LastSuccess == nil {
		t.Fatalf("snapshot: %+v %v", before, err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "native", ConnectionID: conn.ID, ConnectionVersion: conn.Version, SourceObjectID: task.ID, State: "done", TargetVersion: task.TargetVersion}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("- [x] Task 🆔 stable ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.ReconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID != task.ID {
		t.Fatalf("native confirmation: %+v %v", receipt, err)
	}
	after, err := s.WorkbenchSnapshot(conn.ID)
	if err != nil || after.LastSuccess == nil || !after.LastSuccess.Equal(*before.LastSuccess) {
		t.Fatalf("single-note read advanced full scan: %+v %v", after, err)
	}
	found := false
	for _, attempt := range after.Attempts {
		if attempt.Trigger == "write" && attempt.Scope == "notes" && attempt.Coverage == "complete" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing single-note confirmation: %+v", after.Attempts)
	}
}

func TestCreationReadbackUsesVerifiedSingleNote(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	original := []byte("# Tasks\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
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
	now := time.Now().UTC().Add(-time.Minute)
	if err := store.RecordWorkbench(conn, nil, nil, []common.WorkbenchTask{}, &now); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	close(ready)
	s.mu.Lock()
	s.workbenchWatchReady[conn.ID] = workbenchWatchReadiness{version: conn.Version, ready: ready, signaled: true, installed: true, verified: true}
	s.mu.Unlock()
	heading := integrations.WorkbenchObsidianHeadings(original)[0]
	input := common.WorkbenchTaskWrite{RequestID: "create-note", ConnectionID: conn.ID, ConnectionVersion: 1, NotePath: "tasks.md", HeadingPath: heading.HeadingPath, TargetVersion: heading.TargetVersion, Title: "Created"}
	op, _, err := s.admitWorkbenchTaskOperation(input)
	if err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	marker := obsidianCreationMarker(op.Fingerprint)
	if err := os.WriteFile(path, []byte("# Tasks\n- [ ] Created 🆔 "+marker+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.recordWorkbenchWatchEvent(conn.ID, conn.Version, "tasks.md")
	receipt, handled, err := s.reconcileWorkbenchObservedCreation(context.Background(), input.RequestID)
	if err != nil || !handled || receipt.SyncState != "confirmed" || receipt.SourceObjectID == "" {
		t.Fatalf("creation confirmation: %+v handled=%v %v", receipt, handled, err)
	}
	task, err := store.WorkbenchObject(receipt.SourceObjectID)
	if err != nil || task.NativeID != marker || task.Title != "Created" {
		t.Fatalf("created task: %+v %v", task, err)
	}
	snapshot, err := s.WorkbenchSnapshot(conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, attempt := range snapshot.Attempts {
		if attempt.Trigger == "write" && attempt.Scope == "notes" && attempt.Coverage == "complete" {
			found = true
		}
	}
	if !found {
		t.Fatalf("single-note creation attempt missing: %+v", snapshot.Attempts)
	}
}

func TestLocationBoundReconcileRejectsChangedBaselineText(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("# Tasks\n- [ ] Review #old\n"), 0600); err != nil {
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
	before, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(before) != 1 || before[0].NativeID != "" {
		t.Fatalf("tasks: %+v %v", before, err)
	}
	matchWorkbench(conn, nil, before)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, before, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "changed-baseline", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: before[0].ID, State: "done", TargetVersion: workbenchFingerprint(before[0])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Tasks\n- [x] Review #new ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(after) != 1 {
		t.Fatalf("updated source: %+v %v", after, err)
	}
	after[0].ID, after[0].SourceKey = before[0].ID, before[0].SourceKey
	after[0].ConnectionID, after[0].Kind, after[0].Mode = conn.ID, conn.Kind, conn.Mode
	if err := store.RecordWorkbench(conn, nil, before, after, &now); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.ReconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "needs_review" {
		t.Fatalf("changed text was treated as the original task: %+v %v", receipt, err)
	}
}

func TestNativeIDReadbackFallsBackWhenAnotherNoteChanges(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(path, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
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
	before, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil || len(before) != 1 {
		t.Fatalf("tasks: %+v %v", before, err)
	}
	matchWorkbench(conn, nil, before)
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn, nil, nil, before, &now); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	close(ready)
	s.mu.Lock()
	s.workbenchWatchReady[conn.ID] = workbenchWatchReadiness{version: conn.Version, ready: ready, signaled: true, installed: true, verified: true}
	s.mu.Unlock()
	input := common.WorkbenchTaskWrite{RequestID: "duplicate", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: before[0].ID, State: "done", TargetVersion: workbenchFingerprint(before[0])}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	ops := s.store.(workbenchTaskOperationStore)
	claimed, err := ops.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := ops.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("- [x] Task 🆔 stable ✅ 2026-11-20\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "other.md"), []byte("- [ ] Duplicate 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s.recordWorkbenchWatchEvent(conn.ID, conn.Version, "other.md")
	receipt, err := s.ReconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != "needs_review" {
		t.Fatalf("changed other note was ignored: %+v %v", receipt, err)
	}
}
