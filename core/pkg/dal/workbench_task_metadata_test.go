package dal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchMetadataSurvivesConnectionRemoval(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, Name: "Vault"}
	if err := s.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := common.WorkbenchTask{ID: "task", ConnectionID: conn.ID, SourceKey: "key", Title: "Task", State: "open"}
	if err := s.RecordWorkbench(conn, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	var versionBefore int64
	if err := s.db.QueryRow("SELECT snapshot_version FROM workbench_connections WHERE id=?", conn.ID).Scan(&versionBefore); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorkbenchLabels(task.ID, []string{" Work ", "Work"}); err != nil {
		t.Fatal(err)
	}
	labels, err := s.WorkbenchLabels()
	if err != nil || len(labels) != 1 || labels[0].Name != "Work" {
		t.Fatalf("labels: %+v %v", labels, err)
	}
	if err := s.PutWorkbenchAnnotation(task.ID, "note", "hello"); err != nil {
		t.Fatal(err)
	}
	var versionAfter int64
	if err := s.db.QueryRow("SELECT snapshot_version FROM workbench_connections WHERE id=?", conn.ID).Scan(&versionAfter); err != nil || versionAfter != versionBefore {
		t.Fatalf("local metadata changed snapshot version: %d -> %d: %v", versionBefore, versionAfter, err)
	}
	if err := s.PutWorkbenchAnnotation(task.ID, "note", "hello"); err != nil {
		t.Fatal(err)
	}
	items, err := s.WorkbenchAnnotations(task.ID)
	if err != nil || len(items) != 1 || items[0].Body != "hello" {
		t.Fatalf("annotations: %+v %v", items, err)
	}
	if err := s.DeleteWorkbench(conn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkbenchAnnotations(task.ID); err == nil {
		t.Fatal("detached task remained visible")
	}
	if err := s.PutWorkbenchAnnotation(task.ID, "note", "changed"); err == nil {
		t.Fatal("modified detached task")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM workbench_task_annotations WHERE task_id=?", task.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retained annotations: %d %v", count, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM workbench_task_labels WHERE task_id=?", task.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retained label associations: %d %v", count, err)
	}
	reconnected := common.WorkbenchConnection{ID: "new-vault", Kind: common.WorkbenchObsidianTasks, Version: 1, Name: "Vault"}
	if err := s.SaveWorkbench(reconnected, false); err != nil {
		t.Fatal(err)
	}
	newTask := common.WorkbenchTask{ID: "new-task", ConnectionID: reconnected.ID, SourceKey: task.SourceKey, Title: task.Title, State: "open"}
	if err := s.RecordWorkbench(reconnected, nil, nil, []common.WorkbenchTask{newTask}, &now); err != nil {
		t.Fatal(err)
	}
	newLabels, err := s.WorkbenchTaskLabels([]string{newTask.ID})
	if err != nil || len(newLabels[newTask.ID]) != 0 {
		t.Fatalf("detached labels attached to new task: %+v %v", newLabels, err)
	}
	newAnnotations, err := s.WorkbenchAnnotations(newTask.ID)
	if err != nil || len(newAnnotations) != 0 {
		t.Fatalf("detached annotations attached to new task: %+v %v", newAnnotations, err)
	}
	rows, err := s.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("metadata contains foreign key violations")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
