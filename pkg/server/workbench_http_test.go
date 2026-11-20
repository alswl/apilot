package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/config"
	"github.com/alswl/apilot/pkg/integrations"
	"github.com/alswl/apilot/pkg/services"
	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestWorkbenchHTTPContract(t *testing.T) {
	service, err := OpenService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	_, api := humatest.New(t)
	registerWorkbenchHTTP(api, service)
	for _, path := range []string{"/health", "/ready", workbenchPath + "/connectors", workbenchPath + "/connections/local"} {
		if res := api.Get(path); res.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, res.Code, res.Body)
		}
	}
	if res := api.Get(workbenchPath + "/tasks?limit=101"); res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation: %d %s", res.Code, res.Body)
	}
	res := api.Get(workbenchPath + "/tasks/missing")
	if res.Code != http.StatusNotFound || res.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("not found: %d %s", res.Code, res.Body)
	}
}

func TestWorkbenchHTTPDoesNotRequireLegacyTransport(t *testing.T) {
	dir := t.TempDir()
	runtime, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	runtime.server.Close()
	st, err := os.Stat(config.HTTPSocketPath(dir))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("HTTP socket: %v %v", st, err)
	}
	remote := services.NewRemote(config.SocketPath(dir))
	if _, err := remote.Call("status", nil); err == nil {
		t.Fatal("legacy socket is still serving")
	}
	data, err := remote.Call("workbench.task.import", common.WorkbenchImport{Tasks: []common.WorkbenchImportTask{{Key: "http", Title: "HTTP task", State: "open"}}})
	if err != nil {
		t.Fatal(err)
	}
	var connection common.WorkbenchSnapshot
	if err := json.Unmarshal(data, &connection); err != nil || connection.ID != common.DefaultWorkbenchLocalID {
		t.Fatalf("%s: %v", data, err)
	}
	page, err := remote.WorkbenchTasks(common.WorkbenchQuery{Connection: connection.ID})
	if err != nil || page.Total != 1 || page.Items[0].Title != "HTTP task" {
		t.Fatalf("%+v %v", page, err)
	}
	if _, err := remote.WorkbenchTasks(common.WorkbenchQuery{Revision: page.Revision + 1}); err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
		t.Fatalf("business error code was lost: %v", err)
	}
}

func TestWorkbenchLegacyIPCCompatibility(t *testing.T) {
	runtime, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	data, err := integrations.Call(runtime.SocketPath, "workbench.connection.show", map[string]string{"id": "local"})
	if err != nil {
		t.Fatal(err)
	}
	var c common.WorkbenchSnapshot
	if err := json.Unmarshal(data, &c); err != nil || c.ID != common.DefaultWorkbenchLocalID || c.Kind != common.WorkbenchLocal {
		t.Fatalf("legacy connection payload %s: %v", data, err)
	}
}

func TestWorkbenchHTTPSeparatesPluginAndConnectionLifecycle(t *testing.T) {
	runtime, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	remote := services.NewRemote(runtime.SocketPath)
	a, err := remote.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "A", Vault: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := remote.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "B", Vault: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.PauseWorkbench(a.ID, a.Version, true); err != nil {
		t.Fatal(err)
	}
	if err := remote.PauseWorkbench(a.ID, a.Version, false); err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
		t.Fatalf("obsolete pause version: %v", err)
	}
	if err := remote.SyncWorkbench(a.ID); err == nil || common.ProtocolError(err).Code != "CONFLICT" {
		t.Fatalf("paused connection accepted a sync: %v", err)
	}
	current, err := remote.WorkbenchSyncDetails(b.ID)
	if err != nil || current.Paused || !current.Capabilities.Refresh {
		t.Fatalf("independent connection: %+v %v", current, err)
	}
	if err := remote.SetWorkbenchPluginEnabled(common.WorkbenchObsidianTasks, false); err != nil {
		t.Fatal(err)
	}
	current, err = remote.WorkbenchSyncDetails(b.ID)
	if err != nil || !current.PluginDisabled || current.Capabilities.Refresh {
		t.Fatalf("disabled plugin: %+v %v", current, err)
	}
	if err := remote.SetWorkbenchPluginEnabled(common.WorkbenchObsidianTasks, true); err != nil {
		t.Fatal(err)
	}
	current, err = remote.WorkbenchSyncDetails(a.ID)
	if err != nil || !current.Paused || current.PluginDisabled {
		t.Fatalf("pause after re-enable: %+v %v", current, err)
	}
	if err := remote.RemoveWorkbench(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := remote.WorkbenchSyncDetails(a.ID); err == nil || common.ProtocolError(err).Code != "NOT_FOUND" {
		t.Fatalf("removed connection: %v", err)
	}
	if _, err := remote.WorkbenchSyncDetails(b.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchHTTPVersionNegotiation(t *testing.T) {
	handler, _ := workbenchRouter(nil)
	for _, requested := range []string{"", common.WorkbenchAPIVersion, "99"} {
		request := httptest.NewRequest(http.MethodGet, "/health", nil)
		if requested != "" {
			request.Header.Set(common.WorkbenchAPIVersionHeader, requested)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusOK
		if requested == "99" {
			want = http.StatusConflict
		}
		if response.Code != want || response.Header().Get(common.WorkbenchAPIVersionHeader) != common.WorkbenchAPIVersion {
			t.Fatalf("requested %q: %d %s", requested, response.Code, response.Body)
		}
	}
}
