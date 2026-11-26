package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

func TestConnectorScopeEdit(t *testing.T) {
	t.Setenv("APILOT_DEV_CONNECTOR_MANIFEST", "")
	packages, dir := t.TempDir(), t.TempDir()
	const connector = "synthetic-scope-edit"
	cmd := exec.Command("bash", "../../../scripts/build-scoped-connector-fixture.sh", packages, "0.1.0", connector, "scope-edit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build synthetic package: %v\n%s", err, out)
	}
	trust, err := os.ReadFile(filepath.Join(packages, "connector-publishers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "connector-publishers.json"), trust, 0600); err != nil {
		t.Fatal(err)
	}
	service, err := OpenService(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Error(err)
		}
	})
	router, _ := workbenchRouter(service)
	api := uc01TestAPI{t: t, handler: router}
	api.call(http.MethodPost, "/plugins/import", map[string]string{"path": filepath.Join(packages, connector+"-0.1.0.apilotconnector")}, nil)
	var auth common.WorkbenchConnectorAuthorization
	api.call(http.MethodPost, "/connector-authorizations", common.WorkbenchConnectorLoginInput{ConnectorID: connector}, &auth)
	if got := api.status(auth.ID, func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" }); got.Status != "authorized" {
		t.Fatalf("authorization: %+v", got)
	}
	var saved common.WorkbenchConnectorSaveResult
	api.call(http.MethodPut, "/connector-connections", common.WorkbenchConnectorSaveInput{
		AuthorizationID: auth.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1,
		Scopes: []common.WorkbenchConnectorSelection{
			{ScopeType: protocol.ScopeSpace, Config: map[string]any{"target": "alpha", "filter": "all"}, WriteEnabled: true},
			{ScopeType: protocol.ScopeProject, Config: map[string]any{"target": "beta", "filter": "all"}},
		},
	}, &saved)
	scope, sibling := saved.Scopes[0].ScopeID, saved.Scopes[1].ScopeID
	uc01Wait(t, func() bool { return api.scopeTasks(scope).Total == 2 && api.scopeTasks(sibling).Total == 1 })
	beforeTasks := api.scopeTasks(scope).Items
	betaIndex := slices.IndexFunc(beforeTasks, func(task common.WorkbenchTask) bool { return task.NativeID == "beta-1" })
	if betaIndex < 0 {
		t.Fatalf("missing overlap in %+v", beforeTasks)
	}
	beta, alpha := beforeTasks[betaIndex], beforeTasks[1-betaIndex]
	annotations := "/tasks/" + beta.ID + "/annotations"
	api.call(http.MethodPut, annotations+"/qa-note", map[string]string{"body": "Keep this local note"}, nil)

	path := "/connector-scopes/" + scope + "/configuration"
	siblingPath := "/connector-scopes/" + sibling + "/configuration"
	var before, beforeSibling common.WorkbenchConnectorScopeEdit
	api.call(http.MethodGet, path, nil, &before)
	api.call(http.MethodGet, siblingPath, nil, &beforeSibling)
	if before.Boundary.ScopeRef != "space:alpha" || before.Boundary.Config["target"] != "alpha" || before.Boundary.Config["filter"] != "all" ||
		before.ConfigSchema.Fields["filter"].Control != "select" {
		t.Fatalf("edit read: %+v", before)
	}
	var options common.WorkbenchConnectorOptions
	api.call(http.MethodGet, path+"/options?revision=1&field=target&scope_type=space&search=beta", nil, &options)
	if len(options.Items) != 1 || options.Items[0].Value != "beta" || options.SelectedOption == nil || options.SelectedOption.Value != "alpha" || len(options.ConnectedValues) != 0 {
		t.Fatalf("current selection or search result unavailable: %+v", options)
	}
	options = common.WorkbenchConnectorOptions{}
	api.call(http.MethodGet, path+"/options?revision=1&field=target&scope_type=project&search=beta", nil, &options)
	if !slices.Equal(options.ConnectedValues, []string{"beta"}) {
		t.Fatalf("sibling selection should stay connected: %+v", options)
	}

	expect := func(method, path string, input any, status int, fragment string) {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, workbenchPath+path, bytes.NewReader(raw))
		req.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != status || !strings.Contains(response.Body.String(), fragment) {
			t.Fatalf("%s %s: got %d %s, want %d %q", method, path, response.Code, response.Body.String(), status, fragment)
		}
	}

	db, err := sql.Open("sqlite3", filepath.Join(dir, "apilot.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	type stored struct {
		config, ref, access, checkpoint, last, full string
		revision, snapshot, version                 int64
	}
	persisted := func() stored {
		t.Helper()
		var state stored
		err := db.QueryRow(`SELECT config,scope_ref,access_state,sync_checkpoint,last_success,last_full_success,config_revision,snapshot_version,version
			FROM workbench_connections WHERE id=?`, scope).Scan(&state.config, &state.ref, &state.access, &state.checkpoint,
			&state.last, &state.full, &state.revision, &state.snapshot, &state.version)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	baseline := persisted()
	var sourceKey string
	if err := db.QueryRow("SELECT source_key FROM workbench_objects WHERE id=?", beta.ID).Scan(&sourceKey); err != nil || sourceKey != "beta" {
		t.Fatalf("expected stable source key beta, got %q: %v", sourceKey, err)
	}
	assertPrior := func() {
		t.Helper()
		var read common.WorkbenchConnectorScopeEdit
		api.call(http.MethodGet, path, nil, &read)
		if !reflect.DeepEqual(read, before) || persisted() != baseline || api.scopeTasks(scope).Total != 2 {
			t.Fatalf("failed request changed persisted scope: %+v", read)
		}
	}
	draft := common.WorkbenchConnectorScopeDraft{Revision: before.Revision, ScopeType: protocol.ScopeSpace,
		Config: map[string]any{"target": "beta", "filter": "beta-only"}, SchemaVersion: before.ConfigSchema.Version}
	var preview common.WorkbenchConnectorScopePreview
	api.call(http.MethodPost, path+"/preview", draft, &preview)
	if !reflect.DeepEqual(preview.Before, before.Boundary) || preview.After.ScopeRef != "space:beta" ||
		preview.After.Config["filter"] != "beta-only" || preview.PreviewToken == "" {
		t.Fatalf("preview: %+v", preview)
	}
	assertPrior()
	update := common.WorkbenchConnectorScopeUpdate{WorkbenchConnectorScopeDraft: draft, PreviewToken: preview.PreviewToken}
	invalid := update
	invalid.Config = map[string]any{"target": "beta", "filter": "invalid"}
	expect(http.MethodPut, path, invalid, http.StatusBadRequest, "Connector configuration is invalid")
	assertPrior()
	stale := update
	stale.Config = map[string]any{"target": "beta", "filter": "all"}
	expect(http.MethodPut, path, stale, http.StatusConflict, "preview changed")
	assertPrior()

	flag := filepath.Join(dir, "managed-connectors", connector, "deny-scope-access")
	if err := os.WriteFile(flag, nil, 0600); err != nil {
		t.Fatal(err)
	}
	expect(http.MethodPut, path, update, http.StatusForbidden, "access was denied")
	assertPrior()
	if err := os.Remove(flag); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_scope_persist BEFORE UPDATE ON workbench_revision
		BEGIN SELECT RAISE(ABORT, 'synthetic scope persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	expect(http.MethodPut, path, update, http.StatusInternalServerError, "")
	assertPrior()
	if _, err := db.Exec("DROP TRIGGER fail_scope_persist"); err != nil {
		t.Fatal(err)
	}

	detailPath := "/tasks/" + beta.ID + "/detail?segment=fields&scope_id=" + scope
	var beforeDetail common.WorkbenchTaskDetail
	api.call(http.MethodGet, detailPath, nil, &beforeDetail)
	wantTop := []common.WorkbenchTaskTopField{
		{TaskTopField: protocol.TaskTopField{ID: "reporting_project", Title: "Reporting project"}, Value: protocol.Value(protocol.FieldOption, protocol.Option{ID: "reporting", Label: "Task Reporting"})},
		{TaskTopField: protocol.TaskTopField{ID: "external_tier", Title: "Tier"}, Value: protocol.Value(protocol.FieldOption, protocol.Option{ID: "tier-2", Label: "tier-2"})},
		{TaskTopField: protocol.TaskTopField{ID: "flagged", Title: "Flagged"}, Value: protocol.Value(protocol.FieldBoolean, false)},
		{TaskTopField: protocol.TaskTopField{ID: "estimate", Title: "Estimate"}, Value: protocol.Value(protocol.FieldNumber, 0)},
	}
	if !reflect.DeepEqual(beforeDetail.TopFields, wantTop) {
		t.Fatalf("top fields: %+v", beforeDetail.TopFields)
	}
	var alphaDetail common.WorkbenchTaskDetail
	api.call(http.MethodGet, "/tasks/"+alpha.ID+"/detail?segment=fields", nil, &alphaDetail)
	wantAlpha := []common.WorkbenchTaskTopField{
		{TaskTopField: protocol.TaskTopField{ID: "reporting_project", Title: "Reporting project"}, Value: protocol.Value(protocol.FieldOption, protocol.Option{ID: "planning", Label: "Task Planning"})},
		wantTop[2],
	}
	if !reflect.DeepEqual(alphaDetail.TopFields, wantAlpha) {
		t.Fatalf("task-specific and absent fields: %+v", alphaDetail.TopFields)
	}

	var updated common.WorkbenchConnectorScopeUpdateResult
	api.call(http.MethodPut, path, update, &updated)
	if updated.ScopeID != scope || updated.Revision != before.Revision+1 || updated.SyncStatus != "queued" ||
		!reflect.DeepEqual(updated.Boundary, preview.After) {
		t.Fatalf("saved scope: %+v", updated)
	}
	uc01Wait(t, func() bool { return api.scopeTasks(scope).Total == 1 })
	after := api.scopeTasks(scope).Items[0]
	if after.ID != beta.ID || after.NativeID != "beta-1" || after.ConnectionVersion == beta.ConnectionVersion {
		t.Fatalf("overlap did not keep its identity or advance its binding: %+v", after)
	}
	expect(http.MethodGet, "/tasks/"+alpha.ID, nil, http.StatusNotFound, "")
	expect(http.MethodPut, path, update, http.StatusConflict, "scope or schema changed")
	expect(http.MethodPost, "/task-operations", common.WorkbenchTaskWrite{
		RequestID: "old-scope-write", ConnectionID: scope, ConnectionVersion: beta.ConnectionVersion,
		SourceObjectID: beta.ID, TargetVersion: beta.TargetVersion,
		Fields: map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Stale write")},
	}, http.StatusConflict, "Scope write settings changed")
	expect(http.MethodGet, "/tasks/"+beta.ID+"/detail?scope_id="+sibling, nil, http.StatusForbidden, "")
	var notes workbenchItems[common.WorkbenchAnnotation]
	api.call(http.MethodGet, annotations, nil, &notes)
	if len(notes.Items) != 1 || notes.Items[0].Body != "Keep this local note" {
		t.Fatalf("annotation lost: %+v", notes)
	}
	var detail common.WorkbenchTaskDetail
	api.call(http.MethodGet, detailPath, nil, &detail)
	if !reflect.DeepEqual(detail.TopFields, wantTop) {
		t.Fatalf("scope edit changed task top fields: %+v", detail.TopFields)
	}
	var afterSibling common.WorkbenchConnectorScopeEdit
	api.call(http.MethodGet, siblingPath, nil, &afterSibling)
	if api.scopeTasks(sibling).Total != 1 || !reflect.DeepEqual(afterSibling, beforeSibling) {
		t.Fatalf("sibling changed: %+v", afterSibling)
	}
	var overview common.WorkbenchConnectorOverview
	api.call(http.MethodGet, "/connector-connections/"+saved.ConnectionID, nil, &overview)
	for _, candidate := range overview.Scopes {
		if candidate.ScopeID == scope && (candidate.LastRun == nil || candidate.LastRun.Mode != "full" || candidate.LastRun.Status != "success") {
			t.Fatalf("edited scope did not full sync: %+v", candidate)
		}
	}
}
