package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func TestWorkbenchConnectorUC01HTTP(t *testing.T) {
	api, config := prepareUC01TestAPI(t)
	api.scenarios(config)
}

func prepareUC01TestAPI(t *testing.T) (uc01TestAPI, string) {
	t.Helper()
	return prepareNamedConnectorTestAPI(t, "fixture")
}

func prepareNamedConnectorTestAPI(t *testing.T, connectorID string) (uc01TestAPI, string) {
	t.Helper()
	fixtureDir := t.TempDir()
	binary := filepath.Join(fixtureDir, "apilot-fixture")
	cmd := exec.Command("go", "-C", "../../../connectors/protocol", "build", "-ldflags", "-X main.fixtureConnectorID="+connectorID, "-o", binary, "./cmd/apilot-fixture")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build protocol fixture: %v\n%s", err, out)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	manifest := protocol.Manifest{ManifestVersion: 1, ConnectorID: connectorID, ReleaseVersion: "0.1.0", ProtocolVersions: []int{1}, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Entrypoint: "apilot-fixture", EntrypointSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	manifestPath := filepath.Join(fixtureDir, "fixture.json")
	writeUC01JSON(t, manifestPath, manifest)
	t.Setenv("APILOT_DEV_CONNECTOR_MANIFEST", manifestPath)
	dir := t.TempDir()
	s, err := OpenService(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	router, _ := workbenchRouter(s)
	path := filepath.Join(dir, "connectors", connectorID, "fixture.json")
	api := uc01TestAPI{t: t, handler: router}

	return api, path
}

type uc01TestAPI struct {
	t       *testing.T
	handler http.Handler
}

func (api uc01TestAPI) call(method, path string, input, result any) {
	api.t.Helper()
	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			api.t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, workbenchPath+path, bytes.NewReader(body))
	request.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		api.t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
	}
	if result != nil && json.Unmarshal(response.Body.Bytes(), result) != nil {
		api.t.Fatalf("invalid response %s", response.Body.String())
	}
}

func (api uc01TestAPI) status(id string, match func(common.WorkbenchConnectorAuthorization) bool) common.WorkbenchConnectorAuthorization {
	api.t.Helper()
	var result common.WorkbenchConnectorAuthorization
	uc01Wait(api.t, func() bool {
		result = common.WorkbenchConnectorAuthorization{}
		api.call(http.MethodGet, "/connector-authorizations/"+id, nil, &result)
		return match(result)
	})
	return result
}

func (api uc01TestAPI) start(connection string) string {
	api.t.Helper()
	var auth common.WorkbenchConnectorAuthorization
	api.call(http.MethodPost, "/connector-authorizations", common.WorkbenchConnectorLoginInput{ConnectorID: "fixture", ConnectionID: connection}, &auth)
	if auth.ID == "" || auth.Status != "waiting" {
		api.t.Fatalf("unexpected authorization: %+v", auth)
	}
	return auth.ID
}

func (api uc01TestAPI) done(id string, match func(map[string]common.WorkbenchConnectorScopeOverview) bool) map[string]common.WorkbenchConnectorScopeOverview {
	api.t.Helper()
	var scopes map[string]common.WorkbenchConnectorScopeOverview
	uc01Wait(api.t, func() bool {
		var overview common.WorkbenchConnectorOverview
		api.call(http.MethodGet, "/connector-connections/"+id, nil, &overview)
		scopes = map[string]common.WorkbenchConnectorScopeOverview{}
		for _, scope := range overview.Scopes {
			if scope.LastRun == nil || scope.LastRun.Status == "running" {
				return false
			}
			scopes[scope.ScopeRef] = scope
		}
		return len(scopes) == 2 && match(scopes)
	})
	return scopes
}

func (api uc01TestAPI) scenarios(config string) {
	t := api.t
	writeUC01JSON(t, config, map[string]any{"login_delay_ms": 5000})
	id := api.start("")
	first := api.status(id, func(a common.WorkbenchConnectorAuthorization) bool { return a.VerificationURL != "" })
	if first.UserCode == "" {
		t.Fatal("browser code missing")
	}
	var cancelled common.WorkbenchConnectorAuthorization
	api.call(http.MethodDelete, "/connector-authorizations/"+id, nil, &cancelled)
	if cancelled.Status != "cancelled" || cancelled.VerificationURL != "" || cancelled.UserCode != "" {
		t.Fatalf("cancelled: %+v", cancelled)
	}
	var parents workbenchItems[common.WorkbenchConnectorOverview]
	api.call(http.MethodGet, "/connector-connections", nil, &parents)
	if len(parents.Items) != 0 {
		t.Fatal("cancelled login created a connection")
	}

	writeUC01JSON(t, config, map[string]any{"login_result": "expired"})
	expired := api.status(api.start(""), func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" })
	if expired.Status != "expired" || expired.ErrorKind != "auth_required" {
		t.Fatalf("expired: %+v", expired)
	}

	writeUC01JSON(t, config, map[string]any{})
	login := api.status(api.start(""), func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" })
	if login.Status != "authorized" {
		t.Fatalf("login: %+v", login)
	}
	var options protocol.ConfigOptions
	api.call(http.MethodGet, "/connectors/fixture/options?field=project&authorization_id="+login.ID, nil, &options)
	if len(options.Items) != 1 || options.Items[0].Value != "PAY" || options.NextPage == "" {
		t.Fatalf("options: %+v", options)
	}
	api.call(http.MethodGet, "/connectors/fixture/options?field=project&authorization_id="+login.ID+"&page="+options.NextPage, nil, &options)
	if len(options.Items) != 1 || options.Items[0].Value != "OPS" {
		t.Fatalf("options page 2: %+v", options)
	}
	input := common.WorkbenchConnectorSaveInput{AuthorizationID: login.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1}
	for _, name := range []string{"PAY", "OPS", "PAY"} {
		input.Scopes = append(input.Scopes, common.WorkbenchConnectorSelection{Config: map[string]any{"project": name}})
	}
	var healthy common.WorkbenchConnectorSaveResult
	api.call(http.MethodPut, "/connector-connections", input, &healthy)
	healthyRuns := api.done(healthy.ConnectionID, func(_ map[string]common.WorkbenchConnectorScopeOverview) bool { return true })
	for _, scope := range healthyRuns {
		run := scope.LastRun
		if run.Trigger != "initial" || run.Mode != "full" || run.Status != "success" ||
			run.Changed != 2 || scope.LastFullSuccess == nil {
			t.Fatalf("dual-scope first run: %+v", run)
		}
	}
	var connected common.WorkbenchConnectorOptions
	api.call(http.MethodGet, "/connectors/fixture/options?field=project&authorization_id="+login.ID, nil, &connected)
	if len(connected.ConnectedValues) != 1 || connected.ConnectedValues[0] != "PAY" {
		t.Fatalf("options should mark only the connected scope on this page: %+v", connected)
	}
	api.call(http.MethodDelete, "/connector-connections/"+healthy.ConnectionID, nil, nil)

	writeUC01JSON(t, config, map[string]any{"fail_second_page": "OPS"})
	var saved common.WorkbenchConnectorSaveResult
	api.call(http.MethodPut, "/connector-connections", input, &saved)
	if len(saved.Scopes) != 3 || saved.Scopes[0].Status != "queued" || saved.Scopes[1].Status != "queued" ||
		saved.Scopes[2].Status != "duplicate_selection" || saved.Scopes[2].ScopeID != saved.Scopes[0].ScopeID {
		t.Fatalf("duplicate selection: %+v", saved)
	}
	result := api.done(saved.ConnectionID, func(_ map[string]common.WorkbenchConnectorScopeOverview) bool { return true })
	if result["PAY"].LastRun.Status != "success" || result["PAY"].LastRun.Fetched != 2 || result["OPS"].LastRun.Status != "failed" ||
		result["OPS"].LastSuccess != nil || result["OPS"].LastRun.Fetched != 1 || !result["OPS"].LastRun.UsageComplete {
		t.Fatalf("first range isolation: %+v", result)
	}
	var page common.WorkbenchPage
	api.call(http.MethodGet, "/tasks", nil, &page)
	if page.Total != 2 {
		t.Fatalf("staged page became visible: %+v", page)
	}
	firstTasks := append([]common.WorkbenchTask(nil), page.Items...)
	var repeated common.WorkbenchConnectorSaveResult
	api.call(http.MethodPut, "/connector-connections", input, &repeated)
	if repeated.ConnectionID != saved.ConnectionID || repeated.Scopes[0].Status != "already_connected" ||
		repeated.Scopes[1].Status != "already_connected" || repeated.Scopes[1].ScopeID != saved.Scopes[1].ScopeID {
		t.Fatalf("repeat save: %+v", repeated)
	}

	writeUC01JSON(t, config, map[string]any{})
	api.call(http.MethodPost, "/connector-connections/"+saved.ConnectionID+"/sync", nil, nil)
	result = api.done(saved.ConnectionID, func(scopes map[string]common.WorkbenchConnectorScopeOverview) bool {
		return scopes["PAY"].LastRun.Mode == "delta" && scopes["OPS"].LastRun.Status == "success"
	})
	if result["PAY"].LastRun.Changed != 0 || result["PAY"].LastRun.Requests != 1 || result["OPS"].LastRun.Mode != "full" {
		t.Fatalf("retry and empty delta: %+v", result)
	}
	api.call(http.MethodGet, "/tasks", nil, &page)
	if page.Total != 4 {
		t.Fatalf("expected four published tasks: %+v", page)
	}

	successPAY, successOPS := result["PAY"], result["OPS"]
	writeUC01JSON(t, config, map[string]any{"empty_scope": "PAY", "unverified_scope": "PAY", "fail_second_page": "OPS"})
	api.call(http.MethodPost, "/connector-connections/"+saved.ConnectionID+"/sync?mode=full", nil, nil)
	result = api.done(saved.ConnectionID, func(scopes map[string]common.WorkbenchConnectorScopeOverview) bool {
		return scopes["PAY"].LastRun.FailedStage == "resolve" && scopes["OPS"].LastRun.FailedStage == "read"
	})
	for _, previous := range []common.WorkbenchConnectorScopeOverview{successPAY, successOPS} {
		current := result[previous.ScopeRef]
		if current.LastSuccess == nil || !current.LastSuccess.Equal(*previous.LastSuccess) || current.SnapshotVersion != previous.SnapshotVersion {
			t.Fatalf("failed reconciliation changed the last confirmed snapshot: %+v", current)
		}
	}
	api.call(http.MethodGet, "/tasks", nil, &page)
	if page.Total != 4 {
		t.Fatalf("failed reconciliation removed tasks: %+v", page)
	}

	writeUC01JSON(t, config, map[string]any{"empty_scope": "PAY", "denied_scope": "OPS"})
	api.call(http.MethodPost, "/connector-connections/"+saved.ConnectionID+"/sync?mode=full", nil, nil)
	result = api.done(saved.ConnectionID, func(scopes map[string]common.WorkbenchConnectorScopeOverview) bool {
		return scopes["PAY"].LastRun.Mode == "full" && scopes["OPS"].LastRun.Status == "access_denied"
	})
	if result["PAY"].LastRun.Status != "success" || result["PAY"].LastRun.Changed != 2 || result["PAY"].LastRun.Requests != 2 ||
		result["OPS"].AccessState != "access_denied" {
		t.Fatalf("full reconciliation / loss: %+v", result)
	}
	api.call(http.MethodGet, "/tasks", nil, &page)
	if page.Total != 0 {
		t.Fatalf("removed or denied tasks are visible: %+v", page)
	}

	writeUC01JSON(t, config, map[string]any{"account": "wrong-account"})
	wrong := api.status(api.start(saved.ConnectionID), func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" })
	if wrong.Status != "failed" || wrong.ErrorKind != "access_denied" {
		t.Fatalf("relogin accepted a different account: %+v", wrong)
	}
	writeUC01JSON(t, config, map[string]any{})
	same := api.status(api.start(saved.ConnectionID), func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" })
	if same.Status != "authorized" {
		t.Fatalf("same account: %+v", same)
	}
	api.done(saved.ConnectionID, func(scopes map[string]common.WorkbenchConnectorScopeOverview) bool {
		return scopes["PAY"].LastRun.Trigger == "relogin" && scopes["OPS"].LastRun.Status == "success"
	})
	api.call(http.MethodGet, "/tasks", nil, &page)
	if page.Total != 4 || page.Items[2].ID != firstTasks[0].ID || page.Items[3].ID != firstTasks[1].ID {
		t.Fatalf("task IDs changed after return to scope: %+v", page)
	}
}

func uc01Wait(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("timed out waiting for a Connector result")
}

func writeUC01JSON(t *testing.T, path string, input any) {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".next", data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".next", path); err != nil {
		t.Fatal(err)
	}
}

func TestThirdPartyReadOnlyFullSync(t *testing.T) {
	api, config := prepareNamedConnectorTestAPI(t, "external-sample")
	var catalog workbenchItems[services.WorkbenchConnector]
	api.call(http.MethodGet, "/connectors", nil, &catalog)
	found := false
	for _, item := range catalog.Items {
		if item.Kind == "external-sample" {
			found = true
		}
	}
	if !found {
		t.Fatal("third-party connector missing from catalog")
	}
	var auth common.WorkbenchConnectorAuthorization
	api.call(http.MethodPost, "/connector-authorizations", common.WorkbenchConnectorLoginInput{ConnectorID: "external-sample"}, &auth)
	auth = api.status(auth.ID, func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" })
	if auth.Status != "authorized" {
		t.Fatalf("authorization: %+v", auth)
	}
	var invalid common.WorkbenchConnectorSaveResult
	api.call(http.MethodPut, "/connector-connections", common.WorkbenchConnectorSaveInput{AuthorizationID: auth.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1, Scopes: []common.WorkbenchConnectorSelection{{Config: map[string]any{"project": "INVALID"}}}}, &invalid)
	if invalid.ConnectionID != "" || len(invalid.Scopes) != 1 || invalid.Scopes[0].Status != "rejected" {
		t.Fatalf("invalid config: %+v", invalid)
	}
	var saved common.WorkbenchConnectorSaveResult
	api.call(http.MethodPut, "/connector-connections", common.WorkbenchConnectorSaveInput{AuthorizationID: auth.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1, Scopes: []common.WorkbenchConnectorSelection{{Config: map[string]any{"project": "PAY"}, WriteEnabled: true}}}, &saved)
	if saved.ConnectionID == "" || len(saved.Scopes) != 1 {
		t.Fatalf("save: %+v", saved)
	}
	var overview common.WorkbenchConnectorOverview
	uc01Wait(t, func() bool {
		api.call(http.MethodGet, "/connector-connections/"+saved.ConnectionID, nil, &overview)
		return len(overview.Scopes) == 1 && overview.Scopes[0].LastRun != nil && overview.Scopes[0].LastRun.Status == "success"
	})
	var snapshot common.WorkbenchSnapshot
	api.call(http.MethodGet, "/connections/"+overview.Scopes[0].ScopeID, nil, &snapshot)
	if snapshot.WriteEnabled {
		t.Fatal("third-party connection became writable")
	}

	request := httptest.NewRequest(http.MethodPut, workbenchPath+"/connections", bytes.NewBufferString(fmt.Sprintf(`{"id":%q,"kind":"external-sample","name":"External","version":%d,"write_enabled":true}`, snapshot.ID, snapshot.Version)))
	request.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.handler.ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatalf("write permission accepted: %s", response.Body.String())
	}
	runID := overview.Scopes[0].LastRun.ID
	api.call(http.MethodPost, "/connector-connections/"+saved.ConnectionID+"/sync", nil, nil)
	uc01Wait(t, func() bool {
		api.call(http.MethodGet, "/connector-connections/"+saved.ConnectionID, nil, &overview)
		return overview.Scopes[0].LastRun != nil && overview.Scopes[0].LastRun.ID != runID && overview.Scopes[0].LastRun.Status == "success"
	})
	if overview.Scopes[0].LastRun.Mode != "full" {
		t.Fatalf("sync mode: %+v", overview.Scopes[0].LastRun)
	}
	var page common.WorkbenchPage
	api.call(http.MethodGet, "/tasks?scope_id="+overview.Scopes[0].ScopeID, nil, &page)
	if page.Total != 2 {
		t.Fatalf("tasks: %+v", page)
	}
	var detail common.WorkbenchTaskDetail
	api.call(http.MethodGet, "/tasks/"+page.Items[0].ID+"/detail?segment=description", nil, &detail)
	if len(detail.Items) == 0 {
		t.Fatal("description missing")
	}

	writeInput, err := json.Marshal(common.WorkbenchTaskWrite{RequestID: "external-write", ConnectionID: snapshot.ID, ConnectionVersion: snapshot.Version, SourceObjectID: page.Items[0].ID, TargetVersion: page.Items[0].TargetVersion, Title: "Changed"})
	if err != nil {
		t.Fatal(err)
	}
	writeRequest := httptest.NewRequest(http.MethodPost, workbenchPath+"/task-operations", bytes.NewReader(writeInput))
	writeRequest.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
	writeRequest.Header.Set("Content-Type", "application/json")
	writeResponse := httptest.NewRecorder()
	api.handler.ServeHTTP(writeResponse, writeRequest)
	if writeResponse.Code == http.StatusOK {
		t.Fatalf("write accepted: %s", writeResponse.Body.String())
	}
	logData, err := os.ReadFile(filepath.Join(filepath.Dir(config), "requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(logData, []byte(`"method":"tasks.mutate"`)) {
		t.Fatal("mutation reached source")
	}
	taskID := page.Items[0].ID
	api.call(http.MethodPut, "/tasks/"+taskID+"/annotations/note", map[string]string{"body": "Local note"}, nil)
	lastSuccess := *overview.Scopes[0].LastSuccess
	for _, failure := range []map[string]any{{"fail_second_page": "PAY"}, {"empty_scope": "PAY", "unverified_scope": "PAY"}} {
		writeUC01JSON(t, config, failure)
		runID = overview.Scopes[0].LastRun.ID
		api.call(http.MethodPost, "/connector-connections/"+saved.ConnectionID+"/sync", nil, nil)
		uc01Wait(t, func() bool {
			api.call(http.MethodGet, "/connector-connections/"+saved.ConnectionID, nil, &overview)
			return overview.Scopes[0].LastRun != nil && overview.Scopes[0].LastRun.ID != runID && overview.Scopes[0].LastRun.Status == "failed"
		})
		api.call(http.MethodGet, "/tasks?scope_id="+overview.Scopes[0].ScopeID, nil, &page)
		var annotations workbenchItems[common.WorkbenchAnnotation]
		api.call(http.MethodGet, "/tasks/"+taskID+"/annotations", nil, &annotations)
		if len(annotations.Items) != 1 || annotations.Items[0].Body != "Local note" {
			t.Fatalf("annotation lost: %+v", annotations)
		}
		if page.Total != 2 || !overview.Scopes[0].LastSuccess.Equal(lastSuccess) {
			t.Fatalf("failed sync changed snapshot: %+v %+v", page, overview.Scopes[0])
		}
	}

	writeUC01JSON(t, config, map[string]any{})
	runID = overview.Scopes[0].LastRun.ID
	api.call(http.MethodPost, "/connector-connections/"+saved.ConnectionID+"/sync", nil, nil)
	uc01Wait(t, func() bool {
		api.call(http.MethodGet, "/connector-connections/"+saved.ConnectionID, nil, &overview)
		return overview.Scopes[0].LastRun != nil && overview.Scopes[0].LastRun.ID != runID && overview.Scopes[0].LastRun.Status == "success"
	})
}

func TestThirdPartyRejectsInvalidPackage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*protocol.Manifest)
	}{
		{"digest", func(m *protocol.Manifest) { m.EntrypointSHA256 = fmt.Sprintf("%064d", 0) }},
		{"architecture", func(m *protocol.Manifest) { m.Arch = "other" }},
		{"protocol", func(m *protocol.Manifest) { m.ProtocolVersions = []int{999} }},
		{"reserved", func(m *protocol.Manifest) { m.ConnectorID = "local" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "connector")
			data := []byte("not a connector")
			if err := os.WriteFile(binary, data, 0700); err != nil {
				t.Fatal(err)
			}
			manifest := protocol.Manifest{ManifestVersion: 1, ConnectorID: "external-sample", ReleaseVersion: "0.1.0", ProtocolVersions: []int{1}, OS: runtime.GOOS, Arch: runtime.GOARCH, Entrypoint: "connector", EntrypointSHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
			tc.change(&manifest)
			path := filepath.Join(dir, "manifest.json")
			writeUC01JSON(t, path, manifest)
			t.Setenv("APILOT_DEV_CONNECTOR_MANIFEST", path)
			s, err := OpenService(t.TempDir())
			if err == nil {
				s.Close()
				t.Fatalf("invalid %s accepted", tc.name)
			}
		})
	}
}
