package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

func prepareRemainingMVP(t *testing.T) (uc01TestAPI, string, string, map[string]common.WorkbenchConnectorScopeOverview) {
	api, config := prepareUC01TestAPI(t)
	login := api.status(api.start(""), func(a common.WorkbenchConnectorAuthorization) bool { return a.Status != "waiting" })
	if login.Status != "authorized" {
		t.Fatalf("login: %+v", login)
	}
	input := common.WorkbenchConnectorSaveInput{AuthorizationID: login.ID, EndpointSchemaVersion: 1, ScopeSchemaVersion: 1}
	for _, scope := range []string{"PAY", "OPS"} {
		input.Scopes = append(input.Scopes, common.WorkbenchConnectorSelection{Config: map[string]any{"project": scope}, WriteEnabled: true})
	}
	var saved common.WorkbenchConnectorSaveResult
	api.call(http.MethodPut, "/connector-connections", input, &saved)
	result := api.done(saved.ConnectionID, func(scopes map[string]common.WorkbenchConnectorScopeOverview) bool {
		return scopes["PAY"].LastRun.Status == "success" && scopes["OPS"].LastRun.Status == "success"
	})
	return api, config, saved.ConnectionID, result
}

func (api uc01TestAPI) scopeTasks(id string) common.WorkbenchPage {
	var page common.WorkbenchPage
	api.call(http.MethodGet, "/tasks?scope_id="+id+"&sort=due_date&descending=true", nil, &page)
	return page
}

func (api uc01TestAPI) task(id string) common.WorkbenchTask {
	var task common.WorkbenchTask
	api.call(http.MethodGet, "/tasks/"+id, nil, &task)
	return task
}

func (api uc01TestAPI) submit(input common.WorkbenchTaskWrite, waitFor string) common.WorkbenchTaskReceipt {
	api.t.Helper()
	var receipt common.WorkbenchTaskReceipt
	api.call(http.MethodPost, "/task-operations", input, &receipt)
	uc01Wait(api.t, func() bool {
		receipt = common.WorkbenchTaskReceipt{}
		api.call(http.MethodGet, "/task-operations/"+input.RequestID, nil, &receipt)
		if receipt.SyncState == waitFor {
			return true
		}
		switch receipt.SyncState {
		case common.WorkbenchOperationConfirmed, common.WorkbenchOperationConflict, common.WorkbenchOperationNotExecuted:
			api.t.Fatalf("operation %s: got %s (%s), expected %s", input.RequestID, receipt.SyncState, receipt.Reason, waitFor)
		}
		return false
	})
	return receipt
}

func TestWorkbenchRemainingMVPProjectionDetailsMapping(t *testing.T) {
	api, config, id, scopes := prepareRemainingMVP(t)
	pay, ops := scopes["PAY"].ScopeID, scopes["OPS"].ScopeID
	page := api.scopeTasks(pay)
	if page.Total != 2 || page.Items[0].NativeID != "PAY-1" || page.Items[0].DueDate != "2026-12-15" ||
		page.Items[1].DueDate != "" || page.Items[1].Priority == nil || *page.Items[1].Priority != 2 {
		t.Fatalf("scope projection: %+v", page)
	}
	first, second := page.Items[0].ID, page.Items[1].ID
	api.call(http.MethodGet, "/tasks?source=fixture&assignee=Alice&priority=2&state=open&due_before=2026-12-16", nil, &page)
	if page.Total != 2 || page.Items[0].ConnectionID == page.Items[1].ConnectionID {
		t.Fatalf("mixed scope filter: %+v", page)
	}

	api.submit(common.WorkbenchTaskWrite{RequestID: "local-mixed", ConnectionID: common.DefaultWorkbenchLocalID, ConnectionVersion: 1, Title: "Local mixed view"}, "confirmed")
	var github common.WorkbenchSnapshot
	api.call(http.MethodPut, "/connections", map[string]any{"kind": common.WorkbenchGitHubIssues, "name": "GitHub controlled view", "repository": "alswl/issues"}, &github)
	uc01Wait(t, func() bool {
		var mixed common.WorkbenchPage
		api.call(http.MethodGet, "/tasks?sort=due_date", nil, &mixed)
		kinds := map[string]bool{}
		for _, item := range mixed.Items {
			kinds[item.Kind] = true
		}
		return kinds["fixture"] && kinds[common.WorkbenchLocal] && kinds[common.WorkbenchGitHubIssues]
	})

	var detail common.WorkbenchTaskDetail
	api.call(http.MethodGet, "/tasks/"+first+"/detail?segment=fields", nil, &detail)
	if detail.Fields["promised"].Type != protocol.FieldDate {
		t.Fatalf("native detail: %+v", detail)
	}
	api.call(http.MethodGet, "/tasks/"+first+"/detail?segment=description", nil, &detail)
	if len(detail.Items) != 1 || detail.Items[0].Text.Format != "markdown" {
		t.Fatalf("description: %+v", detail)
	}
	for _, segment := range []string{"comments", "attachments", "links", "history"} {
		detail = common.WorkbenchTaskDetail{}
		api.call(http.MethodGet, "/tasks/"+first+"/detail?segment="+segment, nil, &detail)
		if len(detail.Items) != 1 || detail.NextPage == "" || detail.Cached {
			t.Fatalf("detail page: %+v", detail)
		}
		next := detail.NextPage
		detail = common.WorkbenchTaskDetail{}
		api.call(http.MethodGet, "/tasks/"+first+"/detail?segment="+segment+"&page="+next, nil, &detail)
		if len(detail.Items) != 1 || detail.NextPage != "" || detail.Items[0].ID != segment+"-2" {
			t.Fatalf("detail page 2: %+v", detail)
		}
	}
	writeUC01JSON(t, config, map[string]any{"detail_error": "temporary"})
	detail = common.WorkbenchTaskDetail{}
	api.call(http.MethodGet, "/tasks/"+first+"/detail?segment=history", nil, &detail)
	if !detail.Cached || len(detail.Items) != 1 {
		t.Fatalf("cache fallback: %+v", detail)
	}
	writeUC01JSON(t, config, map[string]any{})

	rule := common.WorkbenchMappingInput{Revision: 0, TaskID: first, Rules: map[string]protocol.MappingRule{"due_date": {Field: "promised", Write: true}}}
	var mapped common.WorkbenchMappingResult
	api.call(http.MethodPost, "/connections/"+pay+"/mapping/preview", rule, &mapped)
	if mapped.Sample == nil || mapped.Sample.DueDate != "2026-12-01" || len(mapped.Issues) > 0 || api.task(first).DueDate != "2026-12-15" {
		t.Fatalf("mapping preview: %+v", mapped)
	}
	api.call(http.MethodPut, "/connections/"+pay+"/mapping", rule, &mapped)
	if mapped.Revision != 1 {
		t.Fatalf("mapping revision: %+v", mapped)
	}
	api.done(id, func(items map[string]common.WorkbenchConnectorScopeOverview) bool {
		return items["PAY"].LastRun.Trigger == "mapping"
	})
	if api.task(first).DueDate != "2026-12-01" || api.task(second).DueDate != "2026-12-01" || api.scopeTasks(ops).Items[0].DueDate != "2026-12-15" {
		t.Fatal("mapping crossed scope or lost task identity")
	}
	writeUC01JSON(t, config, map[string]any{"removed_field": "promised"})
	mapped = common.WorkbenchMappingResult{}
	api.call(http.MethodGet, "/connections/"+pay+"/mapping", nil, &mapped)
	if mapped.Issues["due_date"] != "unknown_field" {
		t.Fatalf("removed field not diagnosed: %+v", mapped)
	}
	var meta protocol.EditMetadataResult
	api.call(http.MethodGet, "/tasks/"+first+"/edit-metadata", nil, &meta)
	for _, field := range meta.Fields {
		if field.ID == "due_date" {
			t.Fatal("removed mapping still writable")
		}
	}
	writeUC01JSON(t, config, map[string]any{"detail_error": "denied"})
	denied := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, workbenchPath+"/tasks/"+first+"/detail?segment=history", nil)
	req.Header.Set(common.WorkbenchAPIVersionHeader, common.WorkbenchAPIVersion)
	api.handler.ServeHTTP(denied, req)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("protected detail: %d %s", denied.Code, denied.Body.String())
	}
	if api.scopeTasks(pay).Total != 1 || api.scopeTasks(ops).Total != 2 {
		t.Fatal("object revocation hid the wrong tasks")
	}

}

func TestWorkbenchRemainingMVPWritesAndActions(t *testing.T) {
	api, config, _, scopes := prepareRemainingMVP(t)
	pay := scopes["PAY"].ScopeID
	task := api.scopeTasks(pay).Items[0]
	input := func(id string) common.WorkbenchTaskWrite {
		task = api.task(task.ID)
		return common.WorkbenchTaskWrite{RequestID: id, ConnectionVersion: task.ConnectionVersion, ConnectionID: pay,
			SourceObjectID: task.ID, TargetVersion: task.TargetVersion}
	}
	write := input("six")
	write.Fields = map[string]protocol.FieldValue{
		"title":       protocol.Value(protocol.FieldText, "Edited title"),
		"description": protocol.Value(protocol.FieldRichText, "Edited body"),
		"assignees":   protocol.Value(protocol.FieldAccounts, []protocol.Option{{ID: "bob", Label: "Bob"}}),
		"priority":    protocol.Value(protocol.FieldNumber, 1),
		"due_date":    protocol.Value(protocol.FieldDate, "2026-12-20"),
		"labels":      protocol.Value(protocol.FieldLabels, []string{"new"}),
	}
	confirmed := api.submit(write, "confirmed")
	got := api.task(task.ID)
	if confirmed.SourceObjectID != task.ID || got.Title != "Edited title" || len(got.Assignees) != 1 || got.Assignees[0] != "Bob" ||
		got.Priority == nil || *got.Priority != 1 || got.DueDate != "2026-12-20" || len(got.Labels) != 1 || got.Labels[0] != "new" {
		t.Fatalf("confirmation: %+v %+v", confirmed, got)
	}
	var description common.WorkbenchTaskDetail
	api.call(http.MethodGet, "/tasks/"+task.ID+"/detail?segment=description", nil, &description)
	if len(description.Items) != 1 || string(description.Items[0].Text.Content) != `"Edited body"` {
		t.Fatalf("body: %+v", description)
	}

	writeUC01JSON(t, config, map[string]any{"write_result": "conflict"})
	conflict := input("conflict")
	conflict.Fields = map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Proposed")}
	result := api.submit(conflict, "conflict")
	if result.Conflict == nil || string(result.Conflict.Current["title"].Value) != `"Edited title"` ||
		string(result.Conflict.Proposed["title"].Value) != `"Proposed"` || api.task(task.ID).Title != "Edited title" {
		t.Fatalf("conflict details: %+v", result)
	}
	writeUC01JSON(t, config, map[string]any{"write_result": "lost"})
	lost := input("same-write")
	lost.Fields = map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Confirmed on readback")}
	api.submit(lost, "needs_review")
	var afterReload common.WorkbenchSnapshot
	api.call(http.MethodGet, "/connections/"+pay+"/sync", nil, &afterReload)
	if !slices.ContainsFunc(afterReload.PendingWrites, func(receipt common.WorkbenchTaskReceipt) bool {
		return receipt.RequestID == lost.RequestID && receipt.SyncState == "needs_review"
	}) {
		t.Fatalf("scope status did not restore the original receipt: %+v", afterReload.PendingWrites)
	}
	if api.task(task.ID).Title != "Edited title" {
		t.Fatal("unknown write replaced confirmed value")
	}
	api.submit(lost, "needs_review")
	api.call(http.MethodPost, "/task-operations/"+lost.RequestID+"/reconcile", nil, &result)
	if result.SyncState != "confirmed" || api.task(task.ID).Title != "Confirmed on readback" {
		t.Fatalf("readback: %+v", result)
	}
	writeUC01JSON(t, config, map[string]any{})
	action := input("resolve")
	action.Action = "resolve"
	action.ActionFields = map[string]protocol.FieldValue{"resolution": protocol.Value(protocol.FieldText, "completed")}
	api.submit(action, "confirmed")
	if api.task(task.ID).State != "done" {
		t.Fatal("state was not confirmed")
	}
	reopen := input("reopen")
	reopen.Action = "reopen"
	api.submit(reopen, "confirmed")
	if api.task(task.ID).State != "open" {
		t.Fatal("state was not reopened")
	}

	writeUC01JSON(t, config, map[string]any{"write_result": "lost"})
	oldMapping := input("mapping-changed")
	oldMapping.Fields = map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Unsettled old mapping")}
	api.submit(oldMapping, "needs_review")
	var changed common.WorkbenchMappingResult
	api.call(http.MethodPut, "/connections/"+pay+"/mapping", common.WorkbenchMappingInput{Revision: 0, RestoreDefaults: true}, &changed)
	if changed.Revision != 1 {
		t.Fatalf("mapping revision: %+v", changed)
	}
	api.call(http.MethodPost, "/task-operations/"+oldMapping.RequestID+"/reconcile", nil, &result)
	if result.SyncState != "needs_review" || api.task(task.ID).Title != "Confirmed on readback" {
		t.Fatal("operation confirmed against a changed mapping")
	}
	writeUC01JSON(t, config, map[string]any{})

	var disabled common.WorkbenchSnapshot
	api.call(http.MethodPut, "/connections", map[string]any{"id": pay, "kind": "fixture", "name": "PAY", "version": 1, "write_enabled": false}, &disabled)
	if disabled.WriteEnabled || disabled.Version != 2 {
		t.Fatalf("scope write setting: %+v", disabled)
	}
	var metadata protocol.EditMetadataResult
	api.call(http.MethodGet, "/tasks/"+task.ID+"/edit-metadata", nil, &metadata)
	if len(metadata.Fields) != 0 || len(metadata.Actions) != 0 {
		t.Fatal("disabled scope still offers editing")
	}

	local := api.submit(common.WorkbenchTaskWrite{RequestID: "local-create", ConnectionID: common.DefaultWorkbenchLocalID, ConnectionVersion: 1, Title: "Local before"}, "confirmed")
	before := api.task(local.SourceObjectID)
	edit := common.WorkbenchTaskWrite{RequestID: "local-edit", ConnectionVersion: before.ConnectionVersion,
		SourceObjectID: before.ID, TargetVersion: before.TargetVersion, Fields: map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Local after")}}
	api.submit(edit, "confirmed")
	after := api.task(before.ID)
	if after.Title != "Local after" {
		t.Fatal("Local edit was not applied")
	}
	api.submit(common.WorkbenchTaskWrite{RequestID: "local-done", SourceObjectID: before.ID, ConnectionVersion: after.ConnectionVersion,
		TargetVersion: after.TargetVersion, Action: "done"}, "confirmed")
	if api.task(before.ID).State != "done" {
		t.Fatal("Local action was not applied")
	}
}

func TestWorkbenchRemainingMVPRunsAndUsage(t *testing.T) {
	api, config, id, scopes := prepareRemainingMVP(t)
	pay, ops := scopes["PAY"].ScopeID, scopes["OPS"].ScopeID
	writeUC01JSON(t, config, map[string]any{"rate_limited_scope": "OPS"})
	api.call(http.MethodPost, "/connector-connections/"+id+"/sync", nil, nil)
	runs := api.done(id, func(items map[string]common.WorkbenchConnectorScopeOverview) bool {
		return items["PAY"].LastRun.Mode == "delta" && items["OPS"].LastRun.ErrorKind == "rate_limited"
	})
	failed := runs["OPS"].LastRun
	if failed.RetryAt == nil || failed.RateLimits != 1 || failed.RateLimitBucket != "scope:fixture-read" {
		t.Fatalf("rate limit evidence: %+v", failed)
	}
	var detail common.WorkbenchSyncRun
	api.call(http.MethodGet, "/sync-runs/"+failed.ID, nil, &detail)
	if detail.ID != failed.ID || detail.ScopeID != ops || detail.ConnectionID != id || detail.Requests != failed.Requests {
		t.Fatalf("run detail: %+v", detail)
	}
	writeUC01JSON(t, config, map[string]any{"partial_scope": "PAY"})
	var retry workbenchItems[common.WorkbenchConnectorScopeResult]
	api.call(http.MethodPost, "/connector-connections/"+id+"/sync?mode=full", nil, &retry)
	if retry.Items[0].ScopeRef != "OPS" || retry.Items[0].ErrorKind != "rate_limited" || retry.Items[1].Status != "queued" {
		t.Fatalf("scope bucket: %+v", retry)
	}
	api.done(id, func(items map[string]common.WorkbenchConnectorScopeOverview) bool {
		return items["PAY"].LastRun.FailedStage == "read"
	})
	var history workbenchItems[common.WorkbenchSyncRun]
	api.call(http.MethodGet, "/sync-runs?scope_id="+pay, nil, &history)
	if len(history.Items) != 3 || history.Items[0].UsageComplete || history.Items[0].Requests != 1 || history.Items[1].Changed != 0 {
		t.Fatalf("PAY runs: %+v", history)
	}
	var usage workbenchItems[common.WorkbenchSyncUsage]
	api.call(http.MethodGet, "/sync-usage?connection_id="+id+"&connector_id=fixture", nil, &usage)
	if len(usage.Items) != 4 {
		t.Fatalf("usage groups: %+v", usage)
	}
	for _, item := range usage.Items {
		if item.ScopeID == pay && item.Mode == "full" && (item.Runs != 2 || item.Failed != 1 || item.PartialRuns != 1 || item.Requests != 3) {
			t.Fatalf("PAY usage: %+v", item)
		}
		if item.ScopeID == pay && item.Mode == "delta" && (item.NoChanges != 1 || item.Requests != 1) {
			t.Fatalf("PAY delta: %+v", item)
		}
		if item.ScopeID == ops && item.Mode == "delta" && (item.RateLimits != 1 || item.Failed != 1 || item.Requests != detail.Requests) {
			t.Fatalf("OPS usage: %+v", item)
		}
	}
	api.call(http.MethodGet, "/sync-runs?from="+time.Now().UTC().Add(time.Hour).Format(time.RFC3339), nil, &history)
	if len(history.Items) != 0 {
		t.Fatalf("future range: %+v", history)
	}
}
