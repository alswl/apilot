package server

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/config"
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
	if _, err := remote.WorkbenchTasks(common.WorkbenchQuery{Revision: page.Revision + 1}); common.ProtocolError(err).Code != "STALE_REVISION" {
		t.Fatalf("business error code was lost: %v", err)
	}
}
