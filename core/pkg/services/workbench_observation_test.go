package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestReconcileObservedNoteRequiresFullCoverageForUncertainChanges(t *testing.T) {
	c := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks}
	old := common.WorkbenchTask{ID: "task", ConnectionID: c.ID, NativeID: "native", SourceKey: "native\x000", State: "open", NativeFields: map[string]string{"path": "a.md"}}
	other := common.WorkbenchTask{ID: "other", ConnectionID: c.ID, NativeID: "other", SourceKey: "other\x000", NativeFields: map[string]string{"path": "b.md"}}
	changed := old
	changed.ID = ""
	changed.State = "done"
	changed.SourceKey = "native"
	observation := common.WorkbenchNoteObservation{Path: "a.md", Coverage: common.WorkbenchCoverageRead, Tasks: []common.WorkbenchTask{changed}}
	updates, full := reconcileWorkbenchObservedNote(c, []common.WorkbenchTask{old, other}, observation)
	if full || len(updates) != 1 || updates[0].ID != old.ID || updates[0].State != "done" {
		t.Fatalf("safe update: %+v full=%v", updates, full)
	}
	observation.Tasks = nil
	if _, full := reconcileWorkbenchObservedNote(c, []common.WorkbenchTask{old, other}, observation); !full {
		t.Fatal("missing task requires a scope read")
	}
	observation.Coverage = common.WorkbenchCoverageMissing
	if _, full := reconcileWorkbenchObservedNote(c, []common.WorkbenchTask{old, other}, observation); !full {
		t.Fatal("missing note requires a scope read")
	}
	observation.Coverage = common.WorkbenchCoverageRead
	observation.Tasks = []common.WorkbenchTask{changed}
	duplicate := other
	duplicate.NativeID = old.NativeID
	if _, full := reconcileWorkbenchObservedNote(c, []common.WorkbenchTask{old, duplicate}, observation); !full {
		t.Fatal("duplicate source identity requires a scope read")
	}
}

func TestConnectionReadResultCarriesCoverageEvidence(t *testing.T) {
	vault := t.TempDir()
	path := filepath.Join(vault, "task.md")
	if err := os.WriteFile(path, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 3}
	before, err := readWorkbenchObsidianWrittenVault(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	matchWorkbench(conn, nil, before)
	if err := os.WriteFile(path, []byte("- [x] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fullRead := func(context.Context, common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		return nil, errors.New("full read unavailable")
	}
	result := observeWorkbenchConnection(context.Background(), conn, 7, before, []string{"task.md"}, true, fullRead)
	if result.Err != nil || result.Scope != common.WorkbenchReadNotes || result.Coverage != common.WorkbenchReadComplete ||
		result.ConnectionID != conn.ID || result.ConnectionVersion != 3 || result.SnapshotVersion != 7 || len(result.Notes) != 1 ||
		len(result.Tasks) != 1 || result.Tasks[0].ID != before[0].ID || result.Tasks[0].State != "done" {
		t.Fatalf("single-note result: %+v", result)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result = observeWorkbenchConnection(context.Background(), conn, 7, before, []string{"task.md"}, true, fullRead)
	if result.Err == nil || result.Scope != common.WorkbenchReadFull || result.Coverage != common.WorkbenchReadUnverified ||
		len(result.Notes) != 1 || result.Notes[0].Coverage != common.WorkbenchCoverageMissing {
		t.Fatalf("incomplete fallback result: %+v", result)
	}
}
