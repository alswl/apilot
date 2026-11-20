package services

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type recoveryOrderingStore struct {
	Store
	workbenchStore
	workbenchTaskOperationStore
	workbenchOperationCommitter
	operations      []common.WorkbenchTaskOperation
	acceptedStarted chan struct{}
	reviewStarted   chan struct{}
	releaseReview   chan struct{}
}

func (s *recoveryOrderingStore) PendingWorkbenchTaskOperations() ([]common.WorkbenchTaskOperation, error) {
	return s.operations, nil
}

func (s *recoveryOrderingStore) WorkbenchTaskOperation(id string) (common.WorkbenchTaskOperation, error) {
	if id == "accepted" {
		select {
		case s.acceptedStarted <- struct{}{}:
		default:
		}
	} else {
		select {
		case s.reviewStarted <- struct{}{}:
		default:
		}
		<-s.releaseReview
	}
	return common.WorkbenchTaskOperation{}, common.Err("NOT_FOUND", "test operation")
}

func TestRecoverySchedulesAcceptedBeforeWaitingForReview(t *testing.T) {
	base, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	store, err := base.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "recovery-order", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: t.TempDir(), Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	fake := &recoveryOrderingStore{
		Store: base.store, workbenchStore: store, workbenchTaskOperationStore: base.store.(workbenchTaskOperationStore), workbenchOperationCommitter: base.store.(workbenchOperationCommitter),
		operations: []common.WorkbenchTaskOperation{
			{Phase: common.WorkbenchOperationNeedsReview, Receipt: common.WorkbenchTaskReceipt{RequestID: "review", ConnectionID: conn.ID}},
			{Phase: common.WorkbenchOperationAccepted, Receipt: common.WorkbenchTaskReceipt{RequestID: "accepted", ConnectionID: conn.ID}},
		},
		acceptedStarted: make(chan struct{}, 1), reviewStarted: make(chan struct{}, 1), releaseReview: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Service{store: fake, ctx: ctx, operationRunning: map[string]bool{}, workbenchPlugins: base.workbenchPlugins}
	s.wg.Add(1)
	go s.recoverWorkbenchOperations(fake)
	select {
	case <-fake.reviewStarted:
	case <-time.After(3 * time.Second):
		close(fake.releaseReview)
		s.wg.Wait()
		t.Fatal("reconciliation did not start")
	}
	select {
	case <-fake.acceptedStarted:
	case <-time.After(3 * time.Second):
		close(fake.releaseReview)
		s.wg.Wait()
		t.Fatal("accepted operation was delayed by reconciliation")
	}
	close(fake.releaseReview)
	s.wg.Wait()
}

func TestAdmitWorkbenchTaskOperationIsDurableAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := s.WorkbenchSnapshot(common.DefaultWorkbenchLocalID)
	if err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "request", ConnectionID: connection.ID, ConnectionVersion: connection.Version, Title: "A task"}
	op, created, err := s.admitWorkbenchTaskOperation(input)
	if err != nil || !created || op.Phase != "accepted" {
		t.Fatalf("admit: %+v %v %v", op, created, err)
	}
	receipt, err := s.WorkbenchTaskOperationReceipt(input.RequestID)
	if err != nil || receipt.RequestID != input.RequestID || receipt.SyncState != "accepted" {
		t.Fatalf("receipt: %+v %v", receipt, err)
	}
	if _, created, err := s.admitWorkbenchTaskOperation(input); err != nil || created {
		t.Fatalf("repeat: created=%v err=%v", created, err)
	}
	changed := input
	changed.Title = "A different task"
	if _, _, err := s.admitWorkbenchTaskOperation(changed); err == nil {
		t.Fatal("accepted a changed request with the same ID")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	op, created, err = s.admitWorkbenchTaskOperation(input)
	if err != nil || created || op.Phase != "accepted" || op.Input.Title != input.Title {
		t.Fatalf("recovered request: %+v %v %v", op, created, err)
	}
}

func TestRestartMarksDispatchedOperationForReview(t *testing.T) {
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := s.WorkbenchSnapshot(common.DefaultWorkbenchLocalID)
	if err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "interrupted", ConnectionID: connection.ID, ConnectionVersion: connection.Version, Title: "Task"}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	store := s.store.(workbenchTaskOperationStore)
	if _, err := store.ClaimWorkbenchTaskOperation(input.RequestID); err != nil {
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
	operation, err := s.store.(workbenchTaskOperationStore).WorkbenchTaskOperation(input.RequestID)
	if err != nil || operation.Phase != "needs_review" || operation.Receipt.SyncState != "needs_review" {
		t.Fatalf("interrupted dispatch: %+v %v", operation, err)
	}
}

func TestRestartConfirmsDispatchedCreationWithoutRepeatingIt(t *testing.T) {
	dir := t.TempDir()
	vault := t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(note, []byte("# Tasks\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "restart-creation", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	headings, err := s.WorkbenchHeadings(conn.ID, "tasks.md")
	if err != nil || len(headings) != 1 {
		t.Fatalf("headings: %+v %v", headings, err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "restart-creation-request", ConnectionID: conn.ID, ConnectionVersion: 1, NotePath: "tasks.md", HeadingPath: headings[0].HeadingPath, TargetVersion: headings[0].TargetVersion, Title: "Recovered task"}
	op, _, err := s.admitWorkbenchTaskOperation(input)
	if err != nil {
		t.Fatal(err)
	}
	_, replacement, err := prepareWorkbenchObsidianCreation(conn, input, op.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.(workbenchTaskOperationStore).ClaimWorkbenchTaskOperation(input.RequestID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, replacement, 0600); err != nil {
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
	deadline := time.Now().Add(5 * time.Second)
	var receipt common.WorkbenchTaskReceipt
	for {
		receipt, err = s.WorkbenchTaskOperationReceipt(input.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.SyncState == common.WorkbenchOperationConfirmed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("operation was not recovered: %+v", receipt)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(note)
	if err != nil || strings.Count(string(data), "Recovered task") != 1 || receipt.SourceObjectID == "" {
		t.Fatalf("source or receipt: %q %+v %v", data, receipt, err)
	}
	task, err := s.WorkbenchTask(receipt.SourceObjectID)
	if err != nil || task.NativeID != obsidianCreationMarker(op.Fingerprint) {
		t.Fatalf("recovered task: %+v %v", task, err)
	}
	replay, err := s.SubmitWorkbenchTaskOperation(input)
	if err != nil || replay != receipt {
		t.Fatalf("replay: %+v %v", replay, err)
	}
}

func TestRestartKeepsUnobservedDispatchForReview(t *testing.T) {
	dir := t.TempDir()
	vault := t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	original := []byte("# Tasks\n")
	if err := os.WriteFile(note, original, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "restart-uncertain", Name: "Vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	headings, err := s.WorkbenchHeadings(conn.ID, "tasks.md")
	if err != nil || len(headings) != 1 {
		t.Fatalf("headings: %+v %v", headings, err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "restart-uncertain-request", ConnectionID: conn.ID, ConnectionVersion: 1, NotePath: "tasks.md", HeadingPath: headings[0].HeadingPath, TargetVersion: headings[0].TargetVersion, Title: "Uncertain task"}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.(workbenchTaskOperationStore).ClaimWorkbenchTaskOperation(input.RequestID); err != nil {
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
	receipt, err := s.ReconcileWorkbenchTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != common.WorkbenchOperationNeedsReview {
		t.Fatalf("uncertain operation: %+v %v", receipt, err)
	}
	replay, err := s.SubmitWorkbenchTaskOperation(input)
	if err != nil || replay.SyncState != common.WorkbenchOperationNeedsReview {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	data, err := os.ReadFile(note)
	if err != nil || string(data) != string(original) {
		t.Fatalf("uncertain operation was resent: %q %v", data, err)
	}
}

func TestScheduledOperationReportsUndispatchedFailure(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "missing", Name: "Missing", Kind: common.WorkbenchObsidianTasks, Vault: filepath.Join(t.TempDir(), "absent"), Scope: ".", Version: 1, WriteEnabled: true}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "failure", ConnectionID: conn.ID, ConnectionVersion: 1, Title: "task", NotePath: "task.md"}
	if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
		t.Fatal(err)
	}
	s.scheduleWorkbenchOperation(input.RequestID)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err := s.WorkbenchTaskOperationReceipt(input.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.SyncState == "not_executed" {
			if receipt.Reason == "" {
				t.Fatal("failure has no explanation")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("operation remained accepted after exhausting retries")
}

func TestSubmitReplayAfterTaskDisappears(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := s.WorkbenchSnapshot(common.DefaultWorkbenchLocalID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.SubmitWorkbenchTaskOperation(common.WorkbenchTaskWrite{RequestID: "create", ConnectionID: conn.ID, ConnectionVersion: conn.Version, Title: "Task"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.WorkbenchTask(created.SourceObjectID)
	if err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "finish", ConnectionID: conn.ID, ConnectionVersion: conn.Version, SourceObjectID: task.ID, TargetVersion: task.TargetVersion, State: "done"}
	receipt, err := s.SubmitWorkbenchTaskOperation(input)
	if err != nil || receipt.SyncState != "confirmed" {
		t.Fatalf("finish: %+v %v", receipt, err)
	}
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.WorkbenchObjects(conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.RecordWorkbench(conn.WorkbenchConnection, nil, current, nil, &now); err != nil {
		t.Fatal(err)
	}
	replay, err := s.SubmitWorkbenchTaskOperation(input)
	if err != nil || replay.SourceObjectID != receipt.SourceObjectID || replay.SyncState != "confirmed" {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	changed := input
	changed.State = "open"
	if _, err := s.SubmitWorkbenchTaskOperation(changed); err == nil {
		t.Fatal("accepted changed replay")
	}
}

func TestLocalMetadataDoesNotInvalidateSourceWrite(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := s.WorkbenchSnapshot(common.DefaultWorkbenchLocalID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.SubmitWorkbenchTaskOperation(common.WorkbenchTaskWrite{RequestID: "metadata-create", ConnectionID: conn.ID, ConnectionVersion: conn.Version, Title: "Task"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.WorkbenchTask(created.SourceObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkbenchLabels(before.ID, []string{"local"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutWorkbenchAnnotation(before.ID, "annotation", "text"); err != nil {
		t.Fatal(err)
	}
	after, err := s.WorkbenchTask(before.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.TargetVersion != after.TargetVersion {
		t.Fatal("local metadata changed source target version")
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
	if err != nil || len(page.Items) != 1 || page.Items[0].TargetVersion != before.TargetVersion {
		t.Fatalf("list version: %+v %v", page, err)
	}
	receipt, err := s.SubmitWorkbenchTaskOperation(common.WorkbenchTaskWrite{RequestID: "metadata-finish", ConnectionID: conn.ID, ConnectionVersion: conn.Version, SourceObjectID: before.ID, TargetVersion: before.TargetVersion, State: "done"})
	if err != nil || receipt.SyncState != "confirmed" {
		t.Fatalf("state update: %+v %v", receipt, err)
	}
}

func TestLegacyReceiptRequiresOriginalRequestForReconcile(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	receipt := common.WorkbenchTaskReceipt{RequestID: "legacy-review", ConnectionID: common.DefaultWorkbenchLocalID, SyncState: "needs_review"}
	writer := s.store.(workbenchTaskWriteStore)
	if err := writer.StageWorkbenchTaskWrite("legacy-hash", receipt); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReconcileWorkbenchTaskOperation(context.Background(), receipt.RequestID)
	if err == nil || common.ProtocolError(err).Code != "CONFLICT" || got.SyncState != "needs_review" {
		t.Fatalf("legacy reconcile: %+v %v", got, err)
	}
	saved, err := s.WorkbenchTaskOperationReceipt(receipt.RequestID)
	if err != nil || saved.SyncState != "needs_review" {
		t.Fatalf("legacy receipt lost: %+v %v", saved, err)
	}
}

func TestReconcileRejectsAfterStopAdmission(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.StopAdmission()
	if _, err := s.ReconcileWorkbenchTaskOperation(context.Background(), "request"); err == nil || common.ProtocolError(err).Code != "CONFLICT" {
		t.Fatalf("reconcile after stop: %v", err)
	}
}

func TestObsidianTargetVersionSeparatesIdentityFromLocation(t *testing.T) {
	task := common.WorkbenchTask{ID: "task", Kind: common.WorkbenchObsidianTasks, NativeID: "native", Title: "Task", State: "open", Locator: "old.md:1", NativeFields: map[string]string{"path": "old.md", "file": "old.md", "line": "1", "section": "Old", "heading_path": "[\"Old\"]", "checkbox": " "}}
	version := workbenchTaskTargetVersion(task)
	moved := task
	moved.Locator = "new.md:42"
	moved.NativeFields = maps.Clone(task.NativeFields)
	moved.NativeFields["path"] = "new.md"
	moved.NativeFields["file"] = "new.md"
	moved.NativeFields["line"] = "42"
	moved.NativeFields["section"] = "New"
	moved.NativeFields["heading_path"] = "[\"New\"]"
	if workbenchTaskTargetVersion(moved) != version {
		t.Fatal("location change invalidated task target")
	}
	moved.NativeFields["checkbox"] = "x"
	if workbenchTaskTargetVersion(moved) == version {
		t.Fatal("source status change did not invalidate task target")
	}
	if !workbenchTaskMatchesTarget(task, workbenchFingerprint(task)) {
		t.Fatal("legacy target version was rejected")
	}
}
