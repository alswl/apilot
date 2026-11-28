package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/integrations"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestWorkbenchHTTPContract(t *testing.T) {
	spec, err := WorkbenchOpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"read_method", "bridge_configured", "bridge_endpoint", "bridge_token", "bridge_certificate"} {
		if bytes.Contains(spec, []byte(field+":")) {
			t.Fatalf("internal field %s is in the public contract", field)
		}
	}
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
	if res := api.Get(workbenchPath + "/task-operations/missing"); res.Code != http.StatusNotFound {
		t.Fatalf("missing operation: %d %s", res.Code, res.Body)
	}
	if res := api.Get(workbenchPath + "/github-repositories?authorization_id=unknown"); res.Code != http.StatusUnauthorized {
		t.Fatalf("repository listing without authorization: %d %s", res.Code, res.Body)
	}
	res := api.Get(workbenchPath + "/tasks/missing")
	if res.Code != http.StatusNotFound || res.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("not found: %d %s", res.Code, res.Body)
	}
}

func TestWorkbenchTaskQueryHTTP(t *testing.T) {
	dir := t.TempDir()
	runtime, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	remote := services.NewRemote(config.SocketPath(dir))
	_, err = remote.Call("workbench.task.import", common.WorkbenchImport{Tasks: []common.WorkbenchImportTask{
		{Key: "fixture", Title: "Literal foo:bar", State: "open"},
		{Key: "another", Title: "Another task", State: "done"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	query := common.WorkbenchTaskQuery{Timezone: "UTC", Keyword: "foo:bar", Expression: `task.status == "open"`}
	result, err := remote.QueryWorkbenchTasks(ctx, query)
	if err != nil || result.QueryTotal != 1 || len(result.Items) != 1 || result.Items[0].Task.Title != "Literal foo:bar" {
		t.Fatalf("query route: %+v %v", result, err)
	}
	query = common.WorkbenchTaskQuery{Timezone: "UTC", IssueRepository: "owner//repo", IssueNumber: "17"}
	if _, err := remote.QueryWorkbenchTasks(ctx, query); err == nil || common.ProtocolError(err).Code != "INVALID_ARGUMENT" {
		t.Fatalf("malformed repository passed the HTTP DTO: %v", err)
	}
	query = common.WorkbenchTaskQuery{Timezone: "UTC", Expression: "true &&\n task.unlisted"}
	if result, err := remote.QueryWorkbenchTasks(ctx, query); err == nil || common.ProtocolError(err).Code != "INVALID_ARGUMENT" ||
		common.ProtocolError(err).Details["line"] != float64(2) || len(result.Items) != 0 {
		t.Fatalf("query diagnostics: %+v %v", result, err)
	}
	query.Expression = "true"
	watch := []string{}
	query.WatchIDs = &watch
	if result, err := remote.QueryWorkbenchTasks(ctx, query); err != nil || result.QueryTotal != 0 {
		t.Fatalf("empty watch filter: %+v %v", result, err)
	}
	page, err := remote.WorkbenchTasks(common.WorkbenchQuery{})
	if err != nil || page.Total != 2 {
		t.Fatalf("legacy list changed: %+v %v", page, err)
	}
}

func TestWorkbenchHTTPDoesNotCacheAuthorizationOrTaskResults(t *testing.T) {
	service, err := OpenService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	handler, _ := workbenchRouter(service)
	for _, path := range []string{"/github-authorizations/missing", "/github-repositories?authorization_id=missing", "/tasks"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, workbenchPath+path, nil))
		if got := response.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", path, got)
		}
	}
}

func TestWorkbenchHTTPErrorExposesOnlySafeDetails(t *testing.T) {
	domain := common.Err("CONFLICT", "Repository is already connected")
	domain.Details["connection_id"] = "existing"
	domain.Details["github_token"] = "secret-value"
	problem := workbenchHTTPError(domain)
	data, err := json.Marshal(problem)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("existing")) || bytes.Contains(data, []byte("secret-value")) {
		t.Fatalf("incorrect safe error details: %s", data)
	}
	if status := problem.(huma.StatusError).GetStatus(); status != http.StatusConflict {
		t.Fatalf("status %d", status)
	}
	limited := common.Err("RATE_LIMITED", "Try later")
	limited.Details["retry_after"] = int64(60)
	if status := workbenchHTTPError(limited).(huma.StatusError).GetStatus(); status != http.StatusTooManyRequests {
		t.Fatalf("rate-limit status %d", status)
	}
}

func TestWorkbenchConnectionValidationDoesNotEchoGitHubToken(t *testing.T) {
	router, _ := workbenchRouter(nil)
	request := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, workbenchPath+"/connections", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	res := request(`{"kind":"github_repository","name":"","repository":"owner/repo","github_token":"test-private-token"}`)
	if res.Code < 400 || bytes.Contains(res.Body.Bytes(), []byte("test-private-token")) {
		t.Fatalf("connection validation leaked token: %d %s", res.Code, res.Body)
	}
	res = request(`{"kind":"github_repository","name":"Repo","repository":"owner/repo","github_token":{"secret":"test-private-token"}}`)
	if res.Code < 400 || bytes.Contains(res.Body.Bytes(), []byte("test-private-token")) {
		t.Fatalf("invalid token field leaked sensitive value: %d %s", res.Code, res.Body)
	}
	res = request(`{"kind":"github_repository","name":"Repo","repository":"owner/repo","github_authorization_id":{"secret":"test-private-session"}}`)
	if res.Code < 400 || bytes.Contains(res.Body.Bytes(), []byte("test-private-session")) {
		t.Fatalf("invalid authorization field leaked session: %d %s", res.Code, res.Body)
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
	receipt, err := remote.SubmitWorkbenchTaskOperation(common.WorkbenchTaskWrite{RequestID: "remote-operation", ConnectionID: connection.ID, ConnectionVersion: connection.Version, Title: "Remote operation"})
	if err != nil || receipt.SyncState != "confirmed" {
		t.Fatalf("submit: %+v %v", receipt, err)
	}
	readback, err := remote.WorkbenchTaskOperationReceipt(receipt.RequestID)
	if err != nil || readback.SourceObjectID != receipt.SourceObjectID {
		t.Fatalf("receipt: %+v %v", readback, err)
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
	input := common.WorkbenchTaskWrite{RequestID: "legacy-operation", ConnectionID: c.ID, ConnectionVersion: c.Version, Title: "Legacy transport"}
	data, err = integrations.Call(runtime.SocketPath, "workbench.operation.submit", input)
	var receipt common.WorkbenchTaskReceipt
	if err != nil || json.Unmarshal(data, &receipt) != nil || receipt.SyncState != "confirmed" {
		t.Fatalf("legacy submit: %s %v", data, err)
	}
	for _, method := range []string{"workbench.operation.show", "workbench.operation.reconcile"} {
		data, err = integrations.Call(runtime.SocketPath, method, map[string]string{"id": input.RequestID})
		var replay common.WorkbenchTaskReceipt
		if err != nil || json.Unmarshal(data, &replay) != nil || replay != receipt {
			t.Fatalf("%s: %s %v", method, data, err)
		}
	}
	vault := t.TempDir()
	id, err := runtime.Service.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Old", Vault: vault, ReadMethod: common.WorkbenchObsidianCLI})
	if err != nil {
		t.Fatal(err)
	}
	data, err = integrations.Call(runtime.SocketPath, "workbench.connection.save", map[string]any{"id": id, "kind": common.WorkbenchObsidianTasks, "name": "Edited", "vault": vault, "version": 1})
	if err != nil || bytes.Contains(data, []byte(`"read_method"`)) || bytes.Contains(data, []byte(`"bridge_configured"`)) {
		t.Fatalf("legacy connection edit: %s %v", data, err)
	}
	internal, err := runtime.Service.WorkbenchSnapshot(id)
	if err != nil || internal.ReadMethod != common.WorkbenchObsidianCLI {
		t.Fatalf("legacy edit changed transport: %+v %v", internal, err)
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

func TestWorkbenchMetadataAcrossHTTP(t *testing.T) {
	runtime, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	remote := services.NewRemote(runtime.SocketPath)
	_, err = remote.Call("workbench.task.import", common.WorkbenchImport{Tasks: []common.WorkbenchImportTask{
		{Key: "a", Title: "Alpha", State: "open"}, {Key: "b", Title: "Beta", State: "open"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	page, err := remote.WorkbenchTasks(common.WorkbenchQuery{Limit: 10})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("tasks: %+v %v", page, err)
	}
	id := page.Items[1].ID
	if err := remote.SetWorkbenchLabels(id, []string{"Work"}); err != nil {
		t.Fatal(err)
	}
	labels, err := remote.WorkbenchLabels()
	if err != nil || len(labels) != 1 {
		t.Fatalf("labels: %+v %v", labels, err)
	}
	filtered, err := remote.WorkbenchTasks(common.WorkbenchQuery{LabelID: labels[0].ID, Limit: 1})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != id {
		t.Fatalf("filtered: %+v %v", filtered, err)
	}
	if err := remote.PutWorkbenchAnnotation(id, "note", "Text"); err != nil {
		t.Fatal(err)
	}
	annotations, err := remote.WorkbenchAnnotations(id)
	if err != nil || len(annotations) != 1 || annotations[0].Body != "Text" {
		t.Fatalf("annotations: %+v %v", annotations, err)
	}
	if err := remote.DeleteWorkbenchAnnotation(id, "note"); err != nil {
		t.Fatal(err)
	}
	annotations, err = remote.WorkbenchAnnotations(id)
	if err != nil || len(annotations) != 0 {
		t.Fatalf("deleted annotations: %+v %v", annotations, err)
	}
}

func TestConnectionEditPreservesOmittedLegacyReadMethod(t *testing.T) {
	service, err := OpenService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	router, _ := workbenchRouter(service)
	vault := t.TempDir()
	send := func(body any) common.WorkbenchSnapshot {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPut, workbenchPath+"/connections", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("save: %d %s", res.Code, res.Body.String())
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &fields); err != nil {
			t.Fatal(err)
		}
		if _, ok := fields["read_method"]; ok {
			t.Fatalf("internal read method is public: %s", res.Body.String())
		}
		if _, ok := fields["bridge_configured"]; ok {
			t.Fatalf("internal bridge choice is public: %s", res.Body.String())
		}
		var snapshot common.WorkbenchSnapshot
		if err := json.Unmarshal(res.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	created := send(map[string]any{"kind": common.WorkbenchObsidianTasks, "name": "Vault", "vault": vault, "read_method": common.WorkbenchObsidianCLI})
	updated := send(map[string]any{"id": created.ID, "kind": created.Kind, "name": "Renamed", "vault": vault, "version": created.Version})
	internal, err := service.WorkbenchSnapshot(updated.ID)
	if err != nil || internal.ReadMethod != common.WorkbenchObsidianCLI {
		t.Fatalf("legacy read method lost: %+v %v", internal, err)
	}
	for _, path := range []string{workbenchPath + "/connections", workbenchPath + "/connections/" + updated.ID, workbenchPath + "/connections/" + updated.ID + "/sync"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK || bytes.Contains(res.Body.Bytes(), []byte(`"read_method"`)) || bytes.Contains(res.Body.Bytes(), []byte(`"bridge_configured"`)) {
			t.Fatalf("public connection %s: %d %s", path, res.Code, res.Body.String())
		}
	}
	cleared := send(map[string]any{"id": updated.ID, "kind": updated.Kind, "name": updated.Name, "vault": vault, "version": updated.Version, "read_method": ""})
	internal, err = service.WorkbenchSnapshot(cleared.ID)
	if err != nil || internal.ReadMethod != "" {
		t.Fatalf("explicit legacy read method reset ignored: %+v %v", internal, err)
	}
}

func TestWorkbenchHTTPObsidianOperationReplay(t *testing.T) {
	dir, vault := t.TempDir(), t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(note, []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeStarted := make(chan struct{}, 1)
	releaseWrite := make(chan struct{}, 1)
	bridge := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(vault, strings.TrimPrefix(r.URL.Path, "/vault/"))
		body, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		hash := sha256.Sum256(body)
		etag := fmt.Sprintf("\"%x\"", hash)
		if r.Method == http.MethodGet {
			w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
			w.Header().Set("ETag", etag)
			_, _ = w.Write(body)
			return
		}
		if r.Header.Get("If-Match") != etag {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		writeStarted <- struct{}{}
		select {
		case <-releaseWrite:
		case <-r.Context().Done():
			return
		}
		body, err = io.ReadAll(r.Body)
		if err != nil || os.WriteFile(path, body, 0600) != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer bridge.Close()
	runtime, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	remote := services.NewRemote(runtime.SocketPath)
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: bridge.Certificate().Raw})
	enabled := true
	connection, err := remote.ConfigureWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault}, common.WorkbenchBridgeInput{Endpoint: bridge.URL, Token: "token", Certificate: string(cert), WriteEnabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if connection.BridgeConfigured || connection.ReadMethod != "" || !connection.WriteEnabled {
		t.Fatalf("public connection exposed transport or lost authorization: %+v", connection)
	}
	updated, err := remote.ConfigureWorkbench(common.WorkbenchConnection{ID: connection.ID, Kind: connection.Kind, Name: "Renamed", Vault: vault, Version: connection.Version}, common.WorkbenchBridgeInput{WriteEnabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	internal, err := runtime.Service.WorkbenchSnapshot(connection.ID)
	if err != nil || !internal.BridgeConfigured || !internal.WriteEnabled || !internal.Capabilities.SetState {
		t.Fatalf("existing writable bridge was not retained: %+v %v", internal, err)
	}
	connection = updated
	deadline := time.Now().Add(5 * time.Second)
	var task common.WorkbenchTask
	for time.Now().Before(deadline) {
		page, err := remote.WorkbenchTasks(common.WorkbenchQuery{Connection: connection.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 {
			task = page.Items[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.ID == "" {
		t.Fatal("source task was not imported")
	}
	input := common.WorkbenchTaskWrite{RequestID: "http-async", ConnectionID: connection.ID, ConnectionVersion: connection.Version, SourceObjectID: task.ID, State: "done", TargetVersion: task.TargetVersion}
	accepted, err := remote.SubmitWorkbenchTaskOperation(input)
	if err != nil || accepted.SyncState != "accepted" {
		t.Fatalf("submit: %+v %v", accepted, err)
	}
	select {
	case <-writeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("background write did not start")
	}
	pending, err := remote.WorkbenchTaskOperationReceipt(input.RequestID)
	if err != nil || pending.SyncState != common.WorkbenchOperationDispatching {
		t.Fatalf("pending receipt: %+v %v", pending, err)
	}
	detail, err := remote.WorkbenchSyncDetails(connection.ID)
	if err != nil || len(detail.PendingWrites) != 1 || detail.PendingWrites[0].RequestID != input.RequestID || detail.PendingWrites[0].SyncState != common.WorkbenchOperationDispatching {
		t.Fatalf("pending operation must be recoverable from the connection: %+v %v", detail.PendingWrites, err)
	}
	before, err := remote.WorkbenchTasks(common.WorkbenchQuery{Connection: connection.ID})
	if err != nil || len(before.Items) != 1 || before.Items[0].State != "open" {
		t.Fatalf("unconfirmed state must remain visible: %+v %v", before, err)
	}
	releaseWrite <- struct{}{}
	deadline = time.Now().Add(5 * time.Second)
	var confirmed common.WorkbenchTaskReceipt
	for time.Now().Before(deadline) {
		confirmed, err = remote.WorkbenchTaskOperationReceipt(input.RequestID)
		if err != nil {
			t.Fatal(err)
		}
		if confirmed.SyncState == "confirmed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if confirmed.SyncState != "confirmed" || confirmed.SourceObjectID != task.ID {
		t.Fatalf("operation did not confirm: %+v", confirmed)
	}
	replay, err := remote.SubmitWorkbenchTaskOperation(input)
	if err != nil || replay != confirmed {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	checked, err := remote.ReconcileWorkbenchTaskOperation(input.RequestID)
	if err != nil || checked != confirmed {
		t.Fatalf("reconcile: %+v %v", checked, err)
	}
	source, err := os.ReadFile(note)
	if err != nil || !strings.Contains(string(source), "- [x] Task") {
		t.Fatalf("source: %q %v", source, err)
	}
}
