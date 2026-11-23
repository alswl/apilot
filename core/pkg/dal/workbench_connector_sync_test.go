package dal

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchConnectorSyncRejectsOlderSnapshot(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	parent := common.WorkbenchConnectorConnection{ID: "parent", ConnectorID: "fixture", EndpointRef: "fixture.local", AccountRef: "account"}
	saved, err := store.SaveWorkbenchConnectorConnection(parent, []common.WorkbenchConnectorNewScope{{ID: "scope", Label: "PAY", Context: protocol.ScopeContext{
		ScopeRef: "PAY", EndpointConfig: []byte("{}"), ScopeConfig: []byte(`{"project":"PAY"}`), EndpointSchemaVersion: 1, ScopeSchemaVersion: 1,
	}}})
	if err != nil || len(saved.Scopes) != 1 {
		t.Fatalf("save: %+v %v", saved, err)
	}
	release := common.WorkbenchConnectorRelease{ConnectorID: parent.ConnectorID, ReleaseVersion: "0.1.0", ProtocolVersion: 1, PackageDigest: "hash"}
	if err := store.RegisterWorkbenchPluginRelease("fixture", &release); err != nil {
		t.Fatal(err)
	}
	scope, err := store.WorkbenchConnectorScope("scope")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	finish := now.Add(time.Millisecond)
	run := common.WorkbenchSyncRun{ID: "run", ScopeID: scope.ScopeID, Trigger: "manual", Mode: "full", Status: "running", StartedAt: now}
	if _, err := store.StartWorkbenchConnectorRun(run); err != nil {
		t.Fatal(err)
	}
	commit := common.WorkbenchConnectorSyncCommit{Scope: scope, Run: run, Release: release, PluginID: "fixture", Checkpoint: "new",
		Records: map[string]common.WorkbenchTask{"1": {ConnectionID: scope.ScopeID, SourceKey: "1", Title: "Old observation", State: "open"}}}
	commit.Run.Status, commit.Run.FinishedAt = "success", &finish
	confirmed := now.Add(500 * time.Microsecond)
	if err := store.RecordWorkbenchIfVersion(scope.Config, nil, nil, []common.WorkbenchTask{{
		ID: "confirmed-task", ConnectionID: scope.ScopeID, SourceKey: "1", Title: "Confirmed write", State: "done",
	}}, &confirmed, scope.SnapshotVersion); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitWorkbenchConnectorSync(commit); err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
		t.Fatalf("stale sync commit: %v", err)
	}
	after, err := store.WorkbenchConnectorScope("scope")
	if err != nil || after.Checkpoint != "" || after.LastFullSuccess != nil || after.SnapshotVersion != scope.SnapshotVersion+1 ||
		after.LastSuccess == nil || !after.LastSuccess.Equal(confirmed) {
		t.Fatalf("progress changed: %+v %v", after, err)
	}
	tasks, err := store.WorkbenchConnectorMembers("scope")
	if err != nil || len(tasks) != 1 || tasks[0].ID != "confirmed-task" || tasks[0].Title != "Confirmed write" || tasks[0].State != "done" {
		t.Fatalf("confirmed write changed: %+v %v", tasks, err)
	}
}
