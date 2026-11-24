package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

func TestExternalConnectorScheduledFullSync(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "fixture")
	cmd := exec.Command("go", "-C", "../../../connectors/protocol", "build", "-ldflags", "-X main.fixtureConnectorID=external-sample", "-o", binary, "./cmd/apilot-fixture")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fixture: %v %s", err, out)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	manifest := protocol.Manifest{ManifestVersion: 1, ConnectorID: "external-sample", ReleaseVersion: "0.1.0", ProtocolVersions: []int{1}, OS: runtime.GOOS, Arch: runtime.GOARCH, Entrypoint: "fixture", EntrypointSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APILOT_DEV_CONNECTOR_MANIFEST", path)
	dataDir := t.TempDir()
	s, err := openService(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	auth, err := s.StartWorkbenchConnectorAuthorization(common.WorkbenchConnectorLoginInput{ConnectorID: "external-sample"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for auth.Status == "waiting" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		auth, err = s.WorkbenchConnectorAuthorization(auth.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if auth.Status != "authorized" {
		t.Fatalf("auth: %+v", auth)
	}
	saved, err := s.SaveWorkbenchConnectorConnection(context.Background(), common.WorkbenchConnectorSaveInput{AuthorizationID: auth.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1, Scopes: []common.WorkbenchConnectorSelection{{Config: map[string]any{"project": "PAY"}}}})
	if err != nil {
		t.Fatal(err)
	}
	var overview []common.WorkbenchConnectorOverview
	wait := func(previous string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			overview, err = s.WorkbenchConnectorConnections(saved.ConnectionID)
			if err != nil {
				t.Fatal(err)
			}
			if len(overview) == 1 && len(overview[0].Scopes) == 1 {
				run := overview[0].Scopes[0].LastRun
				if run != nil && run.ID != previous && run.Status == "success" {
					if run.Mode != "full" {
						t.Fatalf("mode: %s", run.Mode)
					}
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("sync timeout: %+v", overview)
	}
	wait("")
	scope, err := s.WorkbenchSnapshot(saved.Scopes[0].ScopeID)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(dataDir, "apilot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec("UPDATE workbench_sync_runs SET started_at=? WHERE id=?", time.Now().Add(-10*time.Minute).UTC().Format(time.RFC3339Nano), overview[0].Scopes[0].LastRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.scheduleWorkbenchConnectorScope(scope, "periodic")
	wait(overview[0].Scopes[0].LastRun.ID)
	if overview[0].Scopes[0].LastRun.Trigger != "periodic" {
		t.Fatalf("trigger: %+v", overview[0].Scopes[0].LastRun)
	}
}
