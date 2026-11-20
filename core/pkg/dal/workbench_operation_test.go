package dal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchOperationAdmissionAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	connection := common.WorkbenchConnection{ID: "vault", Version: 1}
	if err := store.SaveWorkbench(connection, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := common.WorkbenchTask{ID: "task", ConnectionID: connection.ID, SourceKey: "key", State: "open"}
	if err := store.RecordWorkbench(connection, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: connection.ID, ConnectionVersion: 1, SourceObjectID: "task", State: "done", TargetVersion: "v1"}
	operation, created, err := store.AdmitWorkbenchTaskOperation(input, connection.ID, "hash")
	if err != nil || !created || operation.Phase != "accepted" {
		t.Fatalf("admit: %+v %v %v", operation, created, err)
	}
	visible, err := store.WorkbenchPendingWrites(connection.ID)
	if err != nil || len(visible) != 1 || visible[0].SyncState != "accepted" {
		t.Fatalf("accepted operation missing from connection details: %+v %v", visible, err)
	}
	if _, created, err := store.AdmitWorkbenchTaskOperation(input, connection.ID, "hash"); err != nil || created {
		t.Fatalf("repeat: created=%v err=%v", created, err)
	}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, connection.ID, "different"); err == nil {
		t.Fatal("accepted a different request with the same ID")
	}
	stale := input
	stale.RequestID = "stale"
	stale.ConnectionVersion = 2
	if _, _, err := store.AdmitWorkbenchTaskOperation(stale, connection.ID, "stale"); err == nil {
		t.Fatal("accepted an obsolete connection version")
	}
	claimed, err := store.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil || claimed.Phase != "dispatching" {
		t.Fatal(err)
	}
	receipt := claimed.Receipt
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err == nil {
		t.Fatal("accepted duplicate dispatch claim")
	}
	if err := store.RecordWorkbenchIfVersion(connection, nil, []common.WorkbenchTask{task}, nil, &now, 1); err == nil {
		t.Fatal("dispatch did not fence a previous source observation")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err := store.PendingWorkbenchTaskOperations()
	if err != nil || len(pending) != 1 || pending[0].Phase != "dispatching" || pending[0].Input.TargetVersion != "v1" {
		t.Fatalf("recovered: %+v %v", pending, err)
	}
	receipt.SyncState = "needs_review"
	if err := store.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.AdvanceWorkbenchTaskOperation(input.RequestID, "needs_review", "dispatching", receipt); err == nil {
		t.Fatal("allowed an uncertain request to be dispatched again")
	}
	receipt.SyncState = "confirmed"
	task.State = "done"
	if err := store.CommitWorkbenchTaskOperation(connection, nil, nil, []common.WorkbenchTask{task}, now, 1, receipt); err == nil {
		t.Fatal("confirmed using a stale observation")
	}
	stillPending, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || stillPending.Phase != "needs_review" {
		t.Fatalf("stale commit changed receipt: %+v %v", stillPending, err)
	}
	running := common.WorkbenchAttempt{ID: 99, State: "running", Started: now}
	if err := store.RecordWorkbench(connection, []common.WorkbenchAttempt{running}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	writeAttempt := common.WorkbenchAttempt{ID: 100, State: "success", Started: now}
	if err := store.CommitWorkbenchTaskOperation(connection, []common.WorkbenchAttempt{writeAttempt}, nil, []common.WorkbenchTask{task}, now, 2, receipt); err != nil {
		t.Fatal(err)
	}
	connections, err := store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range connections {
		if snapshot.ID == connection.ID && (len(snapshot.Attempts) < 2 || snapshot.Attempts[0].ID != 99 || snapshot.Attempts[0].State != "running" || snapshot.Attempts[1].ID != 100) {
			t.Fatalf("operation replaced active scan: %+v", snapshot.Attempts)
		}
	}
	running.State = "superseded"
	if err := store.FinishWorkbenchAttempt(connection, running); err != nil {
		t.Fatal(err)
	}
	connections, err = store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range connections {
		if snapshot.ID == connection.ID && (len(snapshot.Attempts) < 2 || snapshot.Attempts[0].State != "superseded" || snapshot.Attempts[1].ID != 100) {
			t.Fatalf("finishing scan lost operation history: %+v", snapshot.Attempts)
		}
	}
	confirmed, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || confirmed.Phase != "confirmed" {
		t.Fatalf("operation not confirmed: %+v %v", confirmed, err)
	}
}

func TestWorkbenchOperationMigrationPreservesLegacyReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	connection := common.WorkbenchConnection{ID: "legacy", Version: 1}
	if err := store.SaveWorkbench(connection, false); err != nil {
		t.Fatal(err)
	}
	receipt := common.WorkbenchTaskReceipt{RequestID: "old", ConnectionID: connection.ID, SyncState: "needs_review"}
	if err := store.StageWorkbenchTaskWrite("old-hash", receipt); err != nil {
		t.Fatal(err)
	}
	restoreLegacyWorkbenchSchema(t, store)
	if _, err := store.db.Exec(`DROP INDEX workbench_task_writes_recovery;
ALTER TABLE workbench_task_writes DROP COLUMN input;
ALTER TABLE workbench_task_writes DROP COLUMN phase;
PRAGMA user_version=5;`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	operation, err := store.WorkbenchTaskOperation("old")
	if err != nil || operation.Phase != "legacy" || operation.Fingerprint != "old-hash" || operation.Receipt.SyncState != "needs_review" {
		t.Fatalf("legacy receipt changed: %+v %v", operation, err)
	}
	pending, err := store.PendingWorkbenchTaskOperations()
	if err != nil || len(pending) != 0 {
		t.Fatalf("legacy request cannot be reconstructed for dispatch: %+v %v", pending, err)
	}
}

func TestWorkbenchOperationClaimRejectsRevokedAuthorization(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: conn.ID, ConnectionVersion: conn.Version, Title: "task"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, conn.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	conn.Version++
	conn.WriteEnabled = false
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err == nil {
		t.Fatal("dispatched after write authorization was revoked")
	}
	op, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "accepted" {
		t.Fatalf("failed claim changed request: %+v %v", op, err)
	}
}

func TestWorkbenchOperationClaimRejectsDisabledPlugin(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: conn.ID, ConnectionVersion: 1, Title: "task"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, conn.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkbenchPluginEnabled(conn.Kind, false); err != nil {
		t.Fatal(err)
	}
	disabledInput := input
	disabledInput.RequestID = "disabled-admission"
	if _, _, err := store.AdmitWorkbenchTaskOperation(disabledInput, conn.ID, "another-hash"); err == nil {
		t.Fatal("accepted a request for a disabled connector")
	}
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err == nil {
		t.Fatal("dispatched with disabled connector")
	}
	op, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "accepted" {
		t.Fatalf("operation changed: %+v %v", op, err)
	}
	if err := store.SetWorkbenchPluginEnabled(conn.Kind, true); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkbenchPluginEnabled(conn.Kind, false); err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "confirmed"
	if err := store.CommitWorkbenchTaskOperation(conn, nil, nil, nil, time.Now().UTC(), 1, claimed.Receipt); err == nil {
		t.Fatal("published an observation after connector was disabled")
	}
	op, err = store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "dispatching" {
		t.Fatalf("disabled connector changed receipt: %+v %v", op, err)
	}
}

func TestWorkbenchSyncMigrationRollsBackBeforeRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, WriteEnabled: true, BridgeConfigured: true}
	task := common.WorkbenchTask{ID: "original", ConnectionID: conn.ID, SourceKey: "stable", Title: "Keep", State: "open"}
	now := time.Now().UTC()
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordWorkbench(conn, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	receipt := common.WorkbenchTaskReceipt{RequestID: "old", ConnectionID: conn.ID, SyncState: "needs_review"}
	if err := store.StageWorkbenchTaskWrite("hash", receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("INSERT INTO workbench_obsidian_bridges(connection_id,endpoint,token,certificate) VALUES(?,?,?,?)", conn.ID, "endpoint", "token", "certificate"); err != nil {
		t.Fatal(err)
	}
	restoreLegacyWorkbenchSchema(t, store)
	if _, err := store.db.Exec(`DROP INDEX workbench_task_writes_recovery;
ALTER TABLE workbench_task_writes DROP COLUMN input;
ALTER TABLE workbench_task_writes DROP COLUMN phase;
PRAGMA user_version=5;`); err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(store.db, 6, workbenchSyncSchema+"\nINSERT INTO missing_migration_test_table VALUES (1);"); err == nil {
		t.Fatal("expected injected migration failure")
	}
	var version, tables int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 5 {
		t.Fatalf("version after rollback: %d %v", version, err)
	}
	if err := store.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('workbench_tasks','workbench_objects_v6')").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("migration tables survived rollback: %d %v", tables, err)
	}
	var title, fingerprint, token string
	if err := store.db.QueryRow("SELECT title FROM workbench_objects WHERE id=?", task.ID).Scan(&title); err != nil || title != task.Title {
		t.Fatalf("old object after rollback: %q %v", title, err)
	}
	if err := store.db.QueryRow("SELECT fingerprint FROM workbench_task_writes WHERE request_id=?", receipt.RequestID).Scan(&fingerprint); err != nil || fingerprint != "hash" {
		t.Fatalf("old receipt after rollback: %q %v", fingerprint, err)
	}
	if err := store.db.QueryRow("SELECT token FROM workbench_obsidian_bridges WHERE connection_id=?", conn.ID).Scan(&token); err != nil || token != "token" {
		t.Fatalf("bridge after rollback: %q %v", token, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	migrated, err := store.WorkbenchObject(task.ID)
	if err != nil || migrated.ID != task.ID || migrated.SourceKey != task.SourceKey {
		t.Fatalf("retry changed task: %+v %v", migrated, err)
	}
	operation, err := store.WorkbenchTaskOperation(receipt.RequestID)
	if err != nil || operation.Phase != "legacy" || operation.Fingerprint != "hash" {
		t.Fatalf("retry changed receipt: %+v %v", operation, err)
	}
	connections, err := store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	var authorized bool
	for _, snapshot := range connections {
		if snapshot.ID == conn.ID {
			authorized = snapshot.WriteEnabled && snapshot.BridgeConfigured
		}
	}
	if !authorized {
		t.Fatal("retry changed connection authorization")
	}
	if err := store.db.QueryRow("SELECT token FROM workbench_obsidian_bridges WHERE connection_id=?", conn.ID).Scan(&token); err != nil || token != "token" {
		t.Fatalf("retry changed bridge: %q %v", token, err)
	}
	var violations int
	rows, err := store.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		violations++
	}
	if err := rows.Close(); err != nil || violations != 0 {
		t.Fatalf("foreign key violations: %d %v", violations, err)
	}
}

func TestWorkbenchOperationReadsEarlierFlatInput(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, WriteEnabled: true, Vault: "/vault", Scope: "."}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "old-input", ConnectionID: conn.ID, ConnectionVersion: 1, Title: "Task"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, conn.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("UPDATE workbench_task_writes SET input=? WHERE request_id=?", encode(input), input.RequestID); err != nil {
		t.Fatal(err)
	}
	op, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Input.RequestID != input.RequestID || op.Input.Title != input.Title || op.Binding != nil {
		t.Fatalf("earlier input: %+v %v", op, err)
	}
	pending, err := store.PendingWorkbenchTaskOperations()
	if err != nil || len(pending) != 1 || pending[0].Input.RequestID != input.RequestID || pending[0].Binding != nil {
		t.Fatalf("earlier recovery input: %+v %v", pending, err)
	}
}

func TestSingleNoteOperationCommitsReceiptAndSnapshotAtomically(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, WriteEnabled: true, Vault: "/vault", Scope: "."}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	task := common.WorkbenchTask{ID: "task", ConnectionID: conn.ID, SourceKey: "note\x000", Title: "Task", State: "open"}
	now := time.Now().UTC().Add(-time.Minute)
	if err := store.RecordWorkbench(conn, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: conn.ID, ConnectionVersion: 1, SourceObjectID: task.ID, State: "done"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, conn.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWorkbenchTaskOperationIfSnapshot(input.RequestID, 0); err == nil {
		t.Fatal("dispatched from an older source snapshot")
	}
	stillAccepted, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || stillAccepted.Phase != "accepted" {
		t.Fatalf("stale dispatch changed operation: %+v %v", stillAccepted, err)
	}
	claimed, err := store.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := store.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	conn.Version++
	conn.Paused = true
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	connections, err := store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	var snapshot common.WorkbenchSnapshot
	for _, c := range connections {
		if c.ID == conn.ID {
			snapshot = c
		}
	}
	task.State = "done"
	receipt := claimed.Receipt
	receipt.SyncState = "confirmed"
	attempt := common.WorkbenchAttempt{ID: time.Now().UnixNano(), Trigger: "write", State: "success", Started: time.Now().UTC(), Scope: "notes", Coverage: "complete"}
	if err := store.CommitWorkbenchObservedOperation(conn, snapshot.SnapshotVersion+1, task, attempt, receipt); err == nil {
		t.Fatal("accepted a stale snapshot")
	}
	current, err := store.WorkbenchObject(task.ID)
	if err != nil || current.State != "open" {
		t.Fatalf("stale commit changed task: %+v %v", current, err)
	}
	op, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "needs_review" {
		t.Fatalf("stale commit changed receipt: %+v %v", op, err)
	}
	if err := store.CommitWorkbenchObservedOperation(conn, snapshot.SnapshotVersion, task, attempt, receipt); err != nil {
		t.Fatal(err)
	}
	current, err = store.WorkbenchObject(task.ID)
	if err != nil || current.State != "done" {
		t.Fatalf("task not confirmed: %+v %v", current, err)
	}
	op, err = store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "confirmed" {
		t.Fatalf("receipt not confirmed: %+v %v", op, err)
	}
	connections, err = store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range connections {
		if c.ID == conn.ID && (c.LastSuccess == nil || !c.LastSuccess.Equal(now)) {
			t.Fatalf("single-note confirmation changed full scan time: %+v", c)
		}
	}
}

func TestSingleNoteCreationCommitsReceiptAndSnapshotAtomically(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, WriteEnabled: true, Vault: "/vault", Scope: "."}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	confirmedAt := time.Now().UTC().Add(-time.Minute)
	if err := store.RecordWorkbench(conn, nil, nil, []common.WorkbenchTask{}, &confirmedAt); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "create", ConnectionID: conn.ID, ConnectionVersion: 1, NotePath: "note.md", HeadingPath: []string{"Tasks"}, Title: "Task"}
	fingerprint := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, conn.ID, fingerprint); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimWorkbenchTaskOperation(input.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	claimed.Receipt.SyncState = "needs_review"
	if err := store.AdvanceWorkbenchTaskOperation(input.RequestID, "dispatching", "needs_review", claimed.Receipt); err != nil {
		t.Fatal(err)
	}
	connections, err := store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	var snapshot common.WorkbenchSnapshot
	for _, c := range connections {
		if c.ID == conn.ID {
			snapshot = c
		}
	}
	marker := "apilot-" + fingerprint[:32]
	task := common.WorkbenchTask{ID: "created", ConnectionID: conn.ID, Kind: conn.Kind, NativeID: marker, SourceKey: "task:" + marker + "\x000", Title: "Task", State: "open", NativeFields: map[string]string{"path": "note.md", "heading_path": `["Tasks"]`}}
	receipt := claimed.Receipt
	receipt.SyncState, receipt.SourceObjectID = "confirmed", task.ID
	attempt := common.WorkbenchAttempt{ID: time.Now().UnixNano(), Trigger: "write", State: "success", Started: time.Now().UTC(), Scope: "notes", Coverage: "complete"}
	if err := store.CommitWorkbenchObservedCreation(conn, snapshot.SnapshotVersion+1, task, attempt, receipt); err == nil {
		t.Fatal("accepted stale creation observation")
	}
	if _, err := store.WorkbenchObject(task.ID); err == nil {
		t.Fatal("stale creation published a task")
	}
	op, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "needs_review" {
		t.Fatalf("stale creation settled receipt: %+v %v", op, err)
	}
	if err := store.CommitWorkbenchObservedCreation(conn, snapshot.SnapshotVersion, task, attempt, receipt); err != nil {
		t.Fatal(err)
	}
	current, err := store.WorkbenchObject(task.ID)
	if err != nil || current.NativeID != marker {
		t.Fatalf("created task: %+v %v", current, err)
	}
	op, err = store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != "confirmed" || op.Receipt.SourceObjectID != task.ID {
		t.Fatalf("created receipt: %+v %v", op, err)
	}
	connections, err = store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range connections {
		if c.ID == conn.ID && (c.LastSuccess == nil || !c.LastSuccess.Equal(confirmedAt)) {
			t.Fatalf("single-note creation changed full scan time: %+v", c)
		}
	}
}
