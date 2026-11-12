package dal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func TestPersistenceAndMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apilot.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	task := common.Task{ID: "id", Name: "demo", Executable: "/bin/echo", Args: []string{}, Env: map[string]string{"SECRET": "private"}, Enabled: true, CreatedAt: time.Now()}
	if e = s.SaveTask(task); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ts, e := s.Tasks()
	if e != nil || len(ts) != 1 || ts[0].Env["SECRET"] != "private" {
		t.Fatalf("%+v %v", ts, e)
	}
	if _, e = s.db.Exec("PRAGMA user_version=999"); e != nil {
		t.Fatal(e)
	}
	s.Close()
	if x, e := Open(path); e == nil {
		x.Close()
		t.Fatal("accepted newer schema")
	}
}
func TestTaskConflictAndRollback(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	a := common.Task{ID: "a", Name: "same"}
	if e = s.SaveTask(a); e != nil {
		t.Fatal(e)
	}
	b := common.Task{ID: "b", Name: "same", Schedule: &common.Schedule{ID: "s"}}
	if e = s.SaveTask(b); e == nil {
		t.Fatal("accepted duplicate name")
	}
	ts, e := s.Tasks()
	if e != nil || len(ts) != 1 {
		t.Fatal(ts, e)
	}
}

func TestForeignKeysAndTransactionalScheduleRollback(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.SaveRun(common.Run{ID: "orphan", TaskID: "missing", Status: "queued", CreatedAt: time.Now()}); e == nil {
		t.Fatal("foreign key not enforced")
	}
	a := common.Task{ID: "a", Name: "a", Schedule: &common.Schedule{ID: "schedule"}}
	if e = s.SaveTask(a); e != nil {
		t.Fatal(e)
	}
	b := common.Task{ID: "b", Name: "b", Schedule: &common.Schedule{ID: "schedule"}}
	if e = s.SaveTask(b); e == nil {
		t.Fatal("duplicate schedule allowed")
	}
	tasks, e := s.Tasks()
	if e != nil || len(tasks) != 1 || tasks[0].ID != "a" {
		t.Fatal(tasks, e)
	}
}
