package dal

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestGitHubSyncCursorCommitsWithSnapshot(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	connection := common.WorkbenchConnection{ID: "github", Version: 1}
	if err := store.SaveWorkbench(connection, false); err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`INSERT INTO workbench_github_connections
		(connection_id,repository_id,account_id,account_login,credential_ref,access_state)
		VALUES(?,?,?,?,?,?)`, connection.ID, 10, 20, "user", "keychain-ref", "available")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	task := common.WorkbenchTask{ID: "task", ConnectionID: connection.ID, SourceKey: "issue:1", Title: "A", State: "open"}
	if err := store.CommitWorkbenchGitHubSync(connection, nil, nil, []common.WorkbenchTask{task}, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	progress, err := store.WorkbenchGitHubProgress(connection.ID)
	if err != nil || progress.Cursor == nil || !progress.Cursor.Equal(now) || progress.LastFullSync == nil {
		t.Fatalf("unexpected progress: %+v %v", progress, err)
	}
	changed := task
	changed.State = "done"
	if err := store.CommitWorkbenchGitHubSync(connection, nil, []common.WorkbenchTask{task}, []common.WorkbenchTask{changed}, now.Add(time.Minute), 0, now.Add(time.Minute), false); err == nil {
		t.Fatal("stale snapshot was accepted")
	}
	progress, err = store.WorkbenchGitHubProgress(connection.ID)
	if err != nil || !progress.Cursor.Equal(now) {
		t.Fatalf("stale operation advanced cursor: %+v %v", progress, err)
	}
	got, err := store.WorkbenchObject(task.ID)
	if err != nil || got.State != "open" {
		t.Fatalf("stale operation changed task: %+v %v", got, err)
	}
}

func TestSaveWorkbenchGitHubRejectsDuplicateAndStaleVersion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	connection := common.WorkbenchConnection{ID: "one", Kind: common.WorkbenchGitHubRepository, Version: 1}
	progress := common.WorkbenchGitHubProgress{ConnectionID: "one", RepositoryID: 42, AccountID: 8, AccountLogin: "user", CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(connection, 0, progress); err != nil {
		t.Fatal(err)
	}
	other := connection
	other.ID = "two"
	progress.ConnectionID = other.ID
	if err := store.SaveWorkbenchGitHub(other, 0, progress); err == nil {
		t.Fatal("duplicate repository was accepted")
	}
	progress.ConnectionID = connection.ID
	connection.Version = 2
	if err := store.SaveWorkbenchGitHub(connection, 0, progress); err == nil {
		t.Fatal("stale connection update was accepted")
	}
	if err := store.SaveWorkbenchGitHub(connection, 1, progress); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubAccountSwitchHidesUntilFullSync(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, AccountLogin: "first", CredentialRef: "one", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := common.WorkbenchTask{ID: "task", ConnectionID: c.ID, SourceKey: "issue:1", Title: "A", State: "open"}
	oldAttempt := common.WorkbenchAttempt{ID: 1, State: "success", Error: "first account history", RequestCount: 7}
	if err := store.CommitWorkbenchGitHubSync(c, []common.WorkbenchAttempt{oldAttempt}, nil, []common.WorkbenchTask{task}, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkbenchLabels(task.ID, []string{"private-label"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutWorkbenchAnnotation(task.ID, "private-note", "private annotation"); err != nil {
		t.Fatal(err)
	}
	other := common.WorkbenchConnection{ID: "other", Kind: common.WorkbenchGitHubRepository, Version: 1}
	otherProgress := common.WorkbenchGitHubProgress{ConnectionID: other.ID, RepositoryID: 43, AccountID: 1, CredentialRef: "one", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(other, 0, otherProgress); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitWorkbenchGitHubSync(other, []common.WorkbenchAttempt{oldAttempt}, nil, nil, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	c.Version = 2
	p.AccountID, p.AccountLogin, p.CredentialRef = 2, "second", "two"
	if err := store.SaveWorkbenchGitHub(c, 1, p); err != nil {
		t.Fatal(err)
	}
	snapshots, err := store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]common.WorkbenchSnapshot)
	for _, snapshot := range snapshots {
		byID[snapshot.ID] = snapshot
	}
	switched, unaffected := byID[c.ID], byID[other.ID]
	if switched.Version != 2 || switched.GitHubAccountID != 2 || switched.AccountLogin != "second" ||
		len(switched.Attempts) != 0 || unaffected.GitHubAccountID != 1 ||
		len(unaffected.Attempts) != 1 || unaffected.Attempts[0].RequestCount != 7 {
		t.Fatalf("account switch did not isolate authorization and history: %+v", byID)
	}
	if _, err := store.WorkbenchObject(task.ID); err == nil {
		t.Fatal("previous account's task remains visible")
	}
	labels, err := store.WorkbenchLabels()
	if err != nil || len(labels) != 0 {
		t.Fatalf("previous account's labels remain visible: %+v %v", labels, err)
	}
	if _, err := store.WorkbenchAnnotations(task.ID); err == nil {
		t.Fatal("previous account's annotations remain visible")
	}
	page, err := store.WorkbenchPage(common.WorkbenchQuery{Limit: 10})
	if err != nil || page.Total != 0 {
		t.Fatalf("previous account's task remains listed: %+v %v", page, err)
	}
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || progress.Cursor != nil || progress.LastFullSync != nil || progress.AccessState != "pending" {
		t.Fatalf("baseline not reset: %+v %v", progress, err)
	}
	if err := store.CommitWorkbenchGitHubSync(c, nil, []common.WorkbenchTask{task}, []common.WorkbenchTask{task}, now.Add(time.Second), 2, now.Add(time.Second), true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WorkbenchObject(task.ID); err != nil {
		t.Fatalf("verified task not visible: %v", err)
	}
	labels, err = store.WorkbenchLabels()
	if err != nil || len(labels) != 1 || labels[0].Name != "private-label" {
		t.Fatalf("verified labels not restored: %+v %v", labels, err)
	}
}

func TestGitHubOperationCannotDispatchAfterAccountSwitch(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1, WriteEnabled: true}
	progress := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, AccountLogin: "first", CredentialRef: "one", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, progress); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: c.ID, ConnectionVersion: 1, Title: "A"}
	op, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, "fingerprint")
	if err != nil || op.Binding == nil || op.Binding.AccountID != 1 || op.Binding.RepositoryID != 42 {
		t.Fatalf("missing operation binding: %+v %v", op, err)
	}
	c.Version = 2
	progress.AccountID, progress.AccountLogin, progress.CredentialRef = 2, "second", "two"
	if err := store.SaveWorkbenchGitHub(c, 1, progress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err == nil {
		t.Fatal("operation dispatched after account change")
	}
	// A new account's verified snapshot must not expose the old account's receipt.
	now := time.Now().UTC()
	if err := store.CommitWorkbenchGitHubSync(c, nil, nil, nil, now, 1, now, true); err != nil {
		t.Fatal(err)
	}
	pending, err := store.WorkbenchPendingWrites(c.ID)
	if err != nil || len(pending) != 0 {
		t.Fatalf("old account's receipt is visible: %+v %v", pending, err)
	}
}

func TestGitHubRevocationHidesAndFencesSnapshot(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "key", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := common.WorkbenchTask{ID: "task", ConnectionID: c.ID, SourceKey: "issue:1"}
	old := common.WorkbenchAttempt{ID: 7, State: "failed", Error: "private error"}
	if err := store.CommitWorkbenchGitHubSync(c, []common.WorkbenchAttempt{old}, nil, []common.WorkbenchTask{task}, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	if err := store.InvalidateWorkbenchGitHubAccess(c, 0); err == nil {
		t.Fatal("stale revocation invalidated newer snapshot")
	}
	if err := store.InvalidateWorkbenchGitHubAccess(c, 1); err != nil {
		t.Fatal(err)
	}
	snapshots, err := store.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range snapshots {
		if snapshot.ID == c.ID && (len(snapshot.Attempts) != 0 || snapshot.LastSuccess != nil) {
			t.Fatal("same-account revocation retained old attempt or last success")
		}
	}
	if _, err := store.WorkbenchObject(task.ID); err == nil {
		t.Fatal("revoked task remains visible")
	}
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || progress.AccessState != "unavailable" || progress.Cursor != nil || progress.LastFullSync != nil {
		t.Fatalf("invalid progress: %+v %v", progress, err)
	}
	if err := store.CommitWorkbenchGitHubSync(c, nil, nil, []common.WorkbenchTask{task}, now, 1, now, true); err == nil {
		t.Fatal("stale sync restored revoked access")
	}
}

func TestGitHubOperationEvidenceSurvivesRecovery(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1, WriteEnabled: true}
	progress := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, AccountLogin: "first", CredentialRef: "one", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, progress); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: c.ID, ConnectionVersion: 1, Title: "A"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, "fingerprint"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordWorkbenchGitHubEvidence(input.RequestID, 123, 7); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverWorkbenchTaskOperations(); err != nil {
		t.Fatal(err)
	}
	op, err := store.WorkbenchTaskOperation(input.RequestID)
	if err != nil || op.Phase != common.WorkbenchOperationNeedsReview || op.Binding.IssueID != 123 || op.Binding.IssueNumber != 7 {
		t.Fatalf("evidence did not survive recovery: %+v %v", op, err)
	}
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err == nil {
		t.Fatal("recovered operation became dispatchable")
	}
}

func TestGitHubLateWriteDenialCannotDisableNewAccount(t *testing.T) {
	for _, unauthorized := range []bool{false, true} {
		t.Run(fmt.Sprintf("unauthorized=%t", unauthorized), func(t *testing.T) {
			testGitHubLateWriteDenialCannotDisableNewAccount(t, unauthorized)
		})
	}
}

func testGitHubLateWriteDenialCannotDisableNewAccount(t *testing.T, unauthorized bool) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1, WriteEnabled: true}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "old", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "late", ConnectionID: c.ID, ConnectionVersion: 1, Title: "task"}
	op, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, "hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err != nil {
		t.Fatal(err)
	}
	c.Version = 2
	p.AccountID, p.CredentialRef = 2, "new"
	if err := store.SaveWorkbenchGitHub(c, 1, p); err != nil {
		t.Fatal(err)
	}
	op.Receipt.SyncState = common.WorkbenchOperationNotExecuted
	if err := store.RejectWorkbenchGitHubWrite(input.RequestID, op.Receipt, unauthorized); err != nil {
		t.Fatal(err)
	}
	current, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || current.WriteDenied || current.AccessState != "pending" {
		t.Fatalf("late rejection affected new account: %+v %v", current, err)
	}
}

func TestGitHubPollDeadlineSurvivesReopenAndFencesOldAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "old", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	until := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Millisecond)
	if err := store.RecordWorkbenchGitHubPoll(c, 1, until); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordWorkbenchGitHubPoll(c, 1, until.Add(-time.Minute)); err != nil {
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
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || progress.NextPollAt == nil || !progress.NextPollAt.Equal(until) {
		t.Fatalf("lost or shortened poll deadline: %+v %v", progress, err)
	}
	c.Version = 2
	p.AccountID, p.CredentialRef = 2, "new"
	if err := store.SaveWorkbenchGitHub(c, 1, p); err != nil {
		t.Fatal(err)
	}
	old := c
	old.Version = 1
	if err := store.RecordWorkbenchGitHubPoll(old, 1, until.Add(time.Hour)); err == nil {
		t.Fatal("old account changed new poll deadline")
	}
	progress, err = store.WorkbenchGitHubProgress(c.ID)
	if err != nil || progress.NextPollAt != nil {
		t.Fatalf("account switch retained old poll deadline: %+v %v", progress, err)
	}
}

func TestGitHubReauthorizationChangesQueryRevision(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "key", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := common.WorkbenchTask{ID: "task", ConnectionID: c.ID, SourceKey: "issue:1", Title: "task"}
	if err := store.CommitWorkbenchGitHubSync(c, nil, nil, []common.WorkbenchTask{task}, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	visible, err := store.WorkbenchPage(common.WorkbenchQuery{Limit: 1})
	if err != nil || visible.Total != 1 {
		t.Fatalf("visible page: %+v %v", visible, err)
	}
	c.Version, p.AccessState = 2, "pending"
	if err := store.SaveWorkbenchGitHub(c, 1, p); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WorkbenchPage(common.WorkbenchQuery{Limit: 1, Revision: visible.Revision}); err == nil {
		t.Fatal("old page revision survived authorization change")
	}
	hidden, err := store.WorkbenchPage(common.WorkbenchQuery{Limit: 1})
	if err != nil || hidden.Total != 0 {
		t.Fatalf("pending task visible: %+v %v", hidden, err)
	}
	if err := store.CommitWorkbenchGitHubSync(c, nil, []common.WorkbenchTask{task}, []common.WorkbenchTask{task}, now.Add(time.Second), 1, now.Add(time.Second), true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WorkbenchPage(common.WorkbenchQuery{Limit: 1, Revision: hidden.Revision}); err == nil {
		t.Fatal("hidden page revision survived successful reauthorization")
	}
	page, err := store.WorkbenchPage(common.WorkbenchQuery{Limit: 1})
	if err != nil || page.Total != 1 {
		t.Fatalf("task not visible after authorization: %+v %v", page, err)
	}
}

func TestGitHubV8MigrationPreservesConnectionAndCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 7, AccountLogin: "user", CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	task := common.WorkbenchTask{ID: "task", ConnectionID: c.ID, SourceKey: "issue:1", Title: "Task"}
	if err := store.CommitWorkbenchGitHubSync(c, nil, nil, []common.WorkbenchTask{task}, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkbenchLabels(task.ID, []string{"kept"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP TABLE workbench_github_account_backoffs;
		ALTER TABLE workbench_github_connections DROP COLUMN next_poll_at;
		ALTER TABLE workbench_github_connections DROP COLUMN write_denied;
		PRAGMA user_version=8`); err != nil {
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
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || progress.RepositoryID != 42 || progress.AccountID != 7 || progress.CredentialRef != "ref" || progress.Cursor == nil || !progress.Cursor.Equal(now) || progress.NextPollAt != nil || progress.WriteDenied {
		t.Fatalf("migration changed binding: %+v %v", progress, err)
	}
	labels, err := store.WorkbenchTaskLabels([]string{task.ID})
	if err != nil || len(labels[task.ID]) != 1 || labels[task.ID][0].Name != "kept" {
		t.Fatalf("migration changed metadata: %+v %v", labels, err)
	}
}

func TestGitHubMigrationPreservesLegacySourcesAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, source := range []struct{ id, kind string }{{"obsidian", common.WorkbenchObsidianTasks}, {"fixture", common.WorkbenchGitHubIssues}} {
		c := common.WorkbenchConnection{ID: source.id, Kind: source.kind, Name: source.id, Version: 1}
		if err := store.SaveWorkbench(c, false); err != nil {
			t.Fatal(err)
		}
		task := common.WorkbenchTask{ID: source.id + "-task", ConnectionID: source.id, SourceKey: "stable", Title: source.id}
		if err := store.RecordWorkbench(c, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
			t.Fatal(err)
		}
		if err := store.SetWorkbenchLabels(task.ID, []string{source.id + "-label"}); err != nil {
			t.Fatal(err)
		}
		if err := store.PutWorkbenchAnnotation(task.ID, source.id+"-note", source.id+"-annotation"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec(`DROP TABLE workbench_github_account_backoffs; DROP TABLE workbench_github_connections; PRAGMA user_version=7`); err != nil {
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
	for _, id := range []string{"obsidian", "fixture"} {
		task, err := store.WorkbenchObject(id + "-task")
		if err != nil || task.ConnectionID != id || task.SourceKey != "stable" {
			t.Fatalf("task %s: %+v %v", id, task, err)
		}
		labels, err := store.WorkbenchTaskLabels([]string{task.ID})
		if err != nil || len(labels[task.ID]) != 1 || labels[task.ID][0].Name != id+"-label" {
			t.Fatalf("labels %s: %+v %v", id, labels, err)
		}
		notes, err := store.WorkbenchAnnotations(task.ID)
		if err != nil || len(notes) != 1 || notes[0].Body != id+"-annotation" {
			t.Fatalf("annotations %s: %+v %v", id, notes, err)
		}
	}
}
