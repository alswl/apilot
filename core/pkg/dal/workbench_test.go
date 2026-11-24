package dal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
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
	stale := task
	stale.State = "done"
	if err := s.RecordWorkbenchIfVersion(c, nil, []common.WorkbenchTask{task}, []common.WorkbenchTask{stale}, &now, 0); err == nil {
		t.Fatal("accepted observation based on an older snapshot")
	}
	if current, err := s.WorkbenchObject(task.ID); err != nil || current.State != "open" {
		t.Fatalf("stale observation changed the task: %+v %v", current, err)
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

func restoreLegacyWorkbenchSchema(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.db.Exec(`PRAGMA foreign_keys=OFF;
DROP TABLE workbench_github_connections;
DROP TABLE workbench_task_labels;
DROP TABLE workbench_task_annotations;
DROP TABLE workbench_labels;
CREATE TABLE legacy_objects (id TEXT PRIMARY KEY, connection_id TEXT NOT NULL REFERENCES workbench_connections(id) ON DELETE CASCADE, source_key TEXT NOT NULL, title TEXT NOT NULL, state TEXT NOT NULL, payload TEXT NOT NULL, UNIQUE (connection_id, source_key));
INSERT INTO legacy_objects SELECT id, connection_id, source_key, title, state, payload FROM workbench_objects;
DROP TABLE workbench_objects;
DROP TABLE workbench_tasks;
ALTER TABLE workbench_connections DROP COLUMN snapshot_version;
ALTER TABLE legacy_objects RENAME TO workbench_objects;
CREATE INDEX workbench_object_list ON workbench_objects(connection_id, state, title, id);
PRAGMA foreign_keys=ON;`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchMigratesV2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := openAtSchema(path, 11)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTask(common.Task{ID: "keep", Name: "Keep", Executable: "/bin/echo", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	restoreLegacyWorkbenchSchema(t, s)
	_, err = s.db.Exec("DROP TABLE workbench_objects; DROP TABLE workbench_connections; DROP TABLE workbench_revision; DROP TABLE workbench_plugin_settings; DROP TABLE workbench_task_writes; DROP TABLE workbench_obsidian_bridges; PRAGMA user_version=2")
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
	s, err := openAtSchema(path, 11)
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
	restoreLegacyWorkbenchSchema(t, s)
	if _, err := s.db.Exec("DELETE FROM workbench_connections WHERE id='local'; DROP TABLE workbench_plugin_settings; DROP TABLE workbench_task_writes; DROP TABLE workbench_obsidian_bridges; PRAGMA user_version=3"); err != nil {
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

func TestWorkbenchMigratesV4PluginAndTaskIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	s, err := openAtSchema(path, 11)
	if err != nil {
		t.Fatal(err)
	}
	connection := common.WorkbenchConnection{ID: "named", Kind: common.WorkbenchLocal, Mode: "local", Name: "Named", Version: 1}
	task := common.WorkbenchTask{ID: "task-v4", ConnectionID: connection.ID, Kind: connection.Kind, Mode: connection.Mode, SourceKey: "key\x000", Title: "Keep", State: "open"}
	now := time.Now().UTC()
	attempts := []common.WorkbenchAttempt{{ID: 42, State: "success", Trigger: "import", Started: now, Finished: &now}}
	if err := s.CommitWorkbenchImport(connection, attempts, nil, []common.WorkbenchTask{task}, &now); err != nil {
		t.Fatal(err)
	}
	restoreLegacyWorkbenchSchema(t, s)
	_, err = s.db.Exec("DROP TABLE workbench_plugin_settings; DROP TABLE workbench_task_writes; DROP TABLE workbench_obsidian_bridges; PRAGMA user_version=4")
	if closeErr := s.Close(); err != nil || closeErr != nil {
		t.Fatalf("fixture: %v %v", err, closeErr)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.WorkbenchObject(task.ID)
	if err != nil || got.SourceKey != task.SourceKey || got.ConnectionID != connection.ID {
		t.Fatalf("task: %+v %v", got, err)
	}
	enabled, err := s.WorkbenchPluginEnabled(common.WorkbenchLocal)
	if err != nil || !enabled {
		t.Fatalf("Local enablement: %t %v", enabled, err)
	}
	connections, err := s.WorkbenchConnections()
	if err != nil || len(connections) != 2 || connections[0].ID != common.DefaultWorkbenchLocalID ||
		connections[1].ID != connection.ID || len(connections[1].Attempts) != 1 || connections[1].Attempts[0].ID != 42 {
		t.Fatalf("connections: %+v %v", connections, err)
	}
}

func TestWorkbenchSnapshotReassignsSourceKeysAtomically(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	connection := common.WorkbenchConnection{ID: "obsidian", Version: 1}
	if err := store.SaveWorkbench(connection, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	before := []common.WorkbenchTask{
		{ID: "a", SourceKey: "note:repeat\x000", Title: "Ship 🔁 every day", State: "open", ConnectionID: connection.ID},
		{ID: "b", SourceKey: "note:ship\x000", Title: "Ship", State: "open", ConnectionID: connection.ID},
	}
	if err := store.RecordWorkbench(connection, nil, nil, before, &now); err != nil {
		t.Fatal(err)
	}
	after := append([]common.WorkbenchTask(nil), before...)
	after[0].SourceKey, after[1].SourceKey = "note:ship\x000", "note:ship\x001"
	after[0].Title = "Ship"

	if err := store.RecordWorkbench(connection, nil, before, after, &now); err != nil {
		t.Fatal(err)
	}
	for _, want := range after {
		got, err := store.WorkbenchObject(want.ID)
		if err != nil || got.SourceKey != want.SourceKey || got.Title != want.Title {
			t.Fatalf("task %s: %+v %v", want.ID, got, err)
		}
	}
}

func TestWorkbenchPartialObservationTransaction(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := common.WorkbenchConnection{ID: "partial", Version: 1}
	if err := s.SaveWorkbench(c, false); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	a := common.WorkbenchTask{ID: "a", ConnectionID: c.ID, SourceKey: "a", Title: "A", State: "open"}
	b := common.WorkbenchTask{ID: "b", ConnectionID: c.ID, SourceKey: "b", Title: "B", State: "open"}
	if err := s.RecordWorkbench(c, nil, nil, []common.WorkbenchTask{a, b}, &now); err != nil {
		t.Fatal(err)
	}
	snapshots, err := s.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	var original common.WorkbenchSnapshot
	for _, v := range snapshots {
		if v.ID == c.ID {
			original = v
		}
	}
	a.State = "done"
	if err := s.CommitWorkbenchObservedTasks(c, original.SnapshotVersion, []common.WorkbenchTask{a}); err != nil {
		t.Fatal(err)
	}
	kept, err := s.WorkbenchObject("b")
	if err != nil || kept.State != "open" {
		t.Fatalf("uncovered task: %+v %v", kept, err)
	}
	snapshots, err = s.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	var updated common.WorkbenchSnapshot
	for _, v := range snapshots {
		if v.ID == c.ID {
			updated = v
		}
	}
	if updated.LastSuccess == nil || !updated.LastSuccess.Equal(*original.LastSuccess) || updated.SnapshotVersion != original.SnapshotVersion+1 {
		t.Fatalf("full verification changed: %+v", updated)
	}
	bad := a
	bad.ID = "missing"
	a.State = "open"
	if err := s.CommitWorkbenchObservedTasks(c, updated.SnapshotVersion, []common.WorkbenchTask{a, bad}); err == nil {
		t.Fatal("accepted missing mapping")
	}
	got, err := s.WorkbenchObject("a")
	if err != nil || got.State != "done" {
		t.Fatalf("partial transaction did not roll back: %+v %v", got, err)
	}
	if err := s.CommitWorkbenchObservedTasks(c, original.SnapshotVersion, []common.WorkbenchTask{a}); err == nil {
		t.Fatal("accepted stale partial observation")
	}
	newConfig := c
	newConfig.Version++
	newConfig.Paused = true
	if err := s.SaveWorkbench(newConfig, false); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitWorkbenchObservedTasks(c, updated.SnapshotVersion, []common.WorkbenchTask{a}); err == nil {
		t.Fatal("accepted an observation from the previous connection configuration")
	}
	if err := s.CommitWorkbenchObservedTasks(newConfig, updated.SnapshotVersion, []common.WorkbenchTask{a}); err == nil {
		t.Fatal("accepted an observation for a paused connection")
	}
	got, err = s.WorkbenchObject("a")
	if err != nil || got.State != "done" {
		t.Fatalf("stale configuration changed task: %+v %v", got, err)
	}
}

func TestWorkbenchScopeResetRemovesHistoricalAttemptPaths(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, Scope: "."}
	if err := s.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	attempt := common.WorkbenchAttempt{ID: 1, State: "success", Scope: "notes", Paths: []string{"private/secret.md"}, Coverage: "complete", Started: time.Now().UTC()}
	if err := s.RecordWorkbench(conn, []common.WorkbenchAttempt{attempt}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	conn.Version++
	conn.Scope = "public"
	if err := s.SaveWorkbench(conn, true); err != nil {
		t.Fatal(err)
	}
	connections, err := s.WorkbenchConnections()
	if err != nil {
		t.Fatal(err)
	}
	for _, snapshot := range connections {
		if snapshot.ID == conn.ID {
			if len(snapshot.Attempts) != 1 || len(snapshot.Attempts[0].Paths) != 0 || snapshot.Attempts[0].Scope != "notes" {
				t.Fatalf("scope reset leaked historical paths: %+v", snapshot.Attempts)
			}
			return
		}
	}
	t.Fatal("connection missing")
}
