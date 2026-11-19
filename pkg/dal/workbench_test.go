package dal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func TestWorkbenchSnapshotTransaction(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := common.WorkbenchConnection{ID: "one", Version: 1}
	if err := s.SaveWorkbench(c, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	task := common.WorkbenchTask{ID: "a", SourceKey: "a", Title: "A", ConnectionID: c.ID, State: "open"}
	if err := s.RecordWorkbench(c, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	page, err := s.WorkbenchPage(common.WorkbenchQuery{Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	other := task
	other.ID = "collision"
	if err := s.RecordWorkbench(c, nil, page.Items, []common.WorkbenchTask{task, other}, &now); err == nil {
		t.Fatal("accepted duplicate source key")
	}
	if next, err := s.WorkbenchPage(common.WorkbenchQuery{Limit: 10, Revision: page.Revision}); err != nil || len(next.Items) != 1 {
		t.Fatalf("snapshot changed: %+v %v", next, err)
	}
	newest := c
	newest.Version++
	if err := s.SaveWorkbench(newest, true); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordWorkbench(c, nil, nil, []common.WorkbenchTask{task}, &now); err == nil {
		t.Fatal("accepted obsolete sync")
	}
	if err := s.RecordWorkbench(newest, nil, nil, nil, &now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkbenchPage(common.WorkbenchQuery{Limit: 10, Revision: page.Revision}); err == nil {
		t.Fatal("accepted stale query")
	}
	current, err := s.WorkbenchConnections()
	if err != nil || len(current) != 2 || current[1].ID != c.ID || current[1].LastSuccess == nil {
		t.Fatalf("empty result should be successful: %+v %v", current, err)
	}
}

func TestWorkbenchMigratesV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTask(common.Task{ID: "keep", Name: "Keep", Executable: "/bin/echo", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("DROP TABLE workbench_objects; DROP TABLE workbench_connections; DROP TABLE workbench_revision; PRAGMA user_version=2")
	s.Close()
	if err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tasks, err := s.Tasks()
	if err != nil || len(tasks) != 1 || tasks[0].ID != "keep" {
		t.Fatalf("%+v %v", tasks, err)
	}
	if _, err := s.WorkbenchConnections(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchMigratesImportedConnection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := common.WorkbenchConnection{ID: "legacy", Version: 1, Kind: "local_import", Mode: "import", Name: "Local"}
	task := common.WorkbenchTask{ID: "preserved", ConnectionID: c.ID, SourceKey: "original", Kind: c.Kind, Mode: c.Mode, Title: "Before update", State: "open"}
	now := time.Now().UTC()
	attempts := []common.WorkbenchAttempt{{ID: 1, Trigger: "import", State: "success", Started: now, Finished: &now}}
	if err := s.CommitWorkbenchImport(c, attempts, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("DELETE FROM workbench_connections WHERE id='local'; PRAGMA user_version=3"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	connections, err := s.WorkbenchConnections()
	if err != nil || len(connections) != 2 {
		t.Fatalf("connections=%+v err=%v", connections, err)
	}
	migrated, builtin := connections[0], connections[1]
	if migrated.ID != c.ID || migrated.Kind != common.WorkbenchLocal || len(migrated.Attempts) != 1 ||
		builtin.ID != common.DefaultWorkbenchLocalID || builtin.Name == migrated.Name {
		t.Fatalf("migration did not preserve the existing collection: %+v", connections)
	}
	obj, err := s.WorkbenchObject(task.ID)
	if err != nil || obj.ConnectionID != c.ID || obj.SourceKey != task.SourceKey ||
		obj.Kind != common.WorkbenchLocal || obj.Mode != "local" {
		t.Fatalf("task=%+v err=%v", obj, err)
	}
}
