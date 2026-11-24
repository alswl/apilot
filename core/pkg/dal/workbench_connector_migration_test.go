package dal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchConnectorMigrationPreservesLocalAndObsidian(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := openAtSchema(path, 11)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, c := range []common.WorkbenchConnection{
		{ID: common.DefaultWorkbenchLocalID, Kind: common.WorkbenchLocal, Mode: "local", Version: 1},
		{ID: "vault", Kind: common.WorkbenchObsidianTasks, Mode: "sync", Version: 1},
	} {
		if c.Kind == common.WorkbenchObsidianTasks {
			if err := store.SaveWorkbench(c, false); err != nil {
				t.Fatal(err)
			}
		}
		task := common.WorkbenchTask{ID: c.ID + "-task", ConnectionID: c.ID, SourceKey: "before", Title: "Preserved"}
		if err := store.RecordWorkbench(c, nil, nil, []common.WorkbenchTask{task}, &now); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetWorkbenchPluginEnabled(common.WorkbenchObsidianTasks, false); err != nil {
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

	for _, id := range []string{common.DefaultWorkbenchLocalID, "vault"} {
		task, err := store.WorkbenchObject(id + "-task")
		if err != nil || task.ConnectionID != id || task.SourceKey != "before" || task.Title != "Preserved" {
			t.Fatalf("existing task: %+v, %v", task, err)
		}
	}
	enabled, err := store.WorkbenchPluginEnabled(common.WorkbenchObsidianTasks)
	if err != nil || enabled {
		t.Fatalf("Obsidian enablement: %v, %v", enabled, err)
	}
}

func TestWorkbenchConnectorMigrationConstraints(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, query := range []string{
		"INSERT INTO workbench_connector_connections(id,connector_id,endpoint_ref,account_ref) VALUES('parent','github-issues','github.com','acct')",
		"INSERT INTO workbench_connections(id,config,version,connector_connection_id,scope_ref) VALUES('scope','{}',1,'parent','repo')",
		"INSERT INTO workbench_sync_runs(id,scope_id,trigger,mode,status,started_at) VALUES('run','scope','manual','full','running','2026-11-23')",
	} {
		if _, err := store.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		"INSERT INTO workbench_connector_connections(id,connector_id,endpoint_ref,account_ref) VALUES('duplicate','github-issues','github.com','acct')",
		"INSERT INTO workbench_connections(id,config,version,connector_connection_id,scope_ref) VALUES('duplicate','{}',1,'parent','repo')",
		"UPDATE workbench_connections SET scope_ref=NULL WHERE id='scope'",
		"UPDATE workbench_connections SET connector_connection_id='missing' WHERE id='scope'",
		"DELETE FROM workbench_connector_connections WHERE id='parent'",
		"UPDATE workbench_sync_runs SET scope_id='missing' WHERE id='run'",
	} {
		if _, err := store.db.Exec(query); err == nil {
			t.Errorf("expected constraint failure for %s", query)
		}
	}
}

func TestWorkbenchConnectorReleaseKeepsEnablement(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const id = common.WorkbenchGitHubRepository
	if err := store.SetWorkbenchPluginEnabled(id, false); err != nil {
		t.Fatal(err)
	}
	release := common.WorkbenchConnectorRelease{ConnectorID: "github-issues", ReleaseVersion: "0.1.0", ProtocolVersion: 1, PackageDigest: "digest"}
	if err := store.RegisterWorkbenchPluginRelease(id, &release); err != nil {
		t.Fatal(err)
	}
	var version, digest string
	var protocolVersion int
	var enabled bool
	err = store.db.QueryRow("SELECT enabled,release_version,protocol_version,package_digest FROM workbench_plugin_settings WHERE id=?", id).Scan(&enabled, &version, &protocolVersion, &digest)
	if err != nil || enabled || version != release.ReleaseVersion || protocolVersion != release.ProtocolVersion || digest != release.PackageDigest {
		t.Fatalf("registered release: %v %s %d %s %v", enabled, version, protocolVersion, digest, err)
	}
	if err := store.RegisterWorkbenchPluginRelease(id, nil); err != nil {
		t.Fatal(err)
	}
	var cleared bool
	err = store.db.QueryRow("SELECT enabled,release_version IS NULL AND protocol_version IS NULL AND package_digest IS NULL FROM workbench_plugin_settings WHERE id=?", id).Scan(&enabled, &cleared)
	if err != nil || enabled || !cleared {
		t.Fatalf("cleared release: enabled=%t cleared=%t error=%v", enabled, cleared, err)
	}
}

func TestWorkbenchConnectorEndpointConfigMovesToParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.db")
	store, err := openAtSchema(path, 15)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO workbench_connector_connections(id,connector_id,endpoint_ref,account_ref)
		VALUES ('parent','fixture','endpoint','acct')`); err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"pay", "ops"} {
		if _, err = store.db.Exec(`INSERT INTO workbench_connections(id,config,version,connector_connection_id,scope_ref)
			VALUES (?,json_object('endpoint_config',json('{"site":"one"}'),'endpoint_schema_version',2,
			'scope_config',json_object('project',?),'scope_schema_version',1),1,'parent',?)`, scope, scope, scope); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for _, id := range []string{"pay", "ops"} {
		scope, err := store.WorkbenchConnectorScope(id)
		if err != nil || string(scope.Context.EndpointConfig) != `{"site":"one"}` || scope.Context.EndpointSchemaVersion != 2 ||
			string(scope.Context.ScopeConfig) != `{"project":"`+id+`"}` {
			t.Fatalf("migrated %s: %+v, %v", id, scope.Context, err)
		}
	}
	var duplicates int
	if err := store.db.QueryRow(`SELECT count(*) FROM workbench_connections
		WHERE connector_connection_id='parent' AND json_type(config,'$.endpoint_config') IS NOT NULL`).Scan(&duplicates); err != nil || duplicates != 0 {
		t.Fatalf("scope copies: %d, %v", duplicates, err)
	}
}
