package workbench

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestConnectorScopeConfirmationReturnsToDraft(t *testing.T) {
	old := englishUI.Swap(false)
	t.Cleanup(func() { englishUI.Store(old) })
	before := common.WorkbenchConnectorBoundary{ScopeType: "space", Label: "Alpha", Config: map[string]any{"target": "alpha", "filter": "all"}}
	after := common.WorkbenchConnectorBoundary{ScopeType: "space", Label: "Beta", Config: map[string]any{"target": "beta", "filter": "beta-only"}}
	panel := &connectorLoginPanel{scopeType: "space", form: &connectorConfigForm{scope: after.Config},
		edit: &connectorScopeEditor{saved: common.WorkbenchConnectorScopeEdit{Revision: 1, ConfigSchema: protocol.ConfigSchema{Version: 1}},
			preview: &common.WorkbenchConnectorScopePreview{Before: before, After: after, PreviewToken: "test"}, problem: "Synthetic failure"}}
	m := &mockup{connectorLogin: panel}
	view := ui.NewTester(m.connectorLoginDialog, 680, 580)
	for _, label := range []string{"当前范围", "新范围", "Alpha · 空间", "Beta · 空间", "all", "beta-only", "Synthetic failure"} {
		if !view.HasText(label) {
			t.Fatalf("missing preview text %q in %v", label, view.Texts())
		}
	}
	view.Key(0, ui.KeyEscape)
	draft, valid := panel.scopeEditDraft()
	if !valid || panel.edit.preview != nil || draft.Config["filter"] != "beta-only" || draft.Config["target"] != "beta" {
		t.Fatalf("confirmation discarded draft: %+v", draft)
	}
	view.Key(0, ui.KeyEscape)
	if m.connectorLogin != nil || m.connectorScopeFocus != panel.edit.id {
		t.Fatal("Escape did not close the editor")
	}
}

func TestConnectorTaskHeaderAboveDescription(t *testing.T) {
	m := connectorUIFixture()
	m.live = &services.Remote{}
	task := m.objects[0]
	detail := m.taskDetailUI(task.ID)
	detail.connectorHeader = &connectorTaskDetailState{version: task.TargetVersion, scopeID: task.ConnectionID, connectionVersion: task.ConnectionVersion,
		segment: "top_fields", loaded: true, page: common.WorkbenchTaskDetail{TopFields: []common.WorkbenchTaskTopField{
			{TaskTopField: protocol.TaskTopField{ID: "project", Title: "Reporting project"}, Value: protocol.Value(protocol.FieldOption, protocol.Option{ID: "reporting", Label: "Task Reporting"})},
			{TaskTopField: protocol.TaskTopField{ID: "tier", Title: "Tier"}, Value: protocol.Value(protocol.FieldOption, protocol.Option{ID: "tier-2"})},
			{TaskTopField: protocol.TaskTopField{ID: "flagged", Title: "Flagged"}, Value: protocol.Value(protocol.FieldBoolean, false)},
			{TaskTopField: protocol.TaskTopField{ID: "estimate", Title: "Estimate"}, Value: protocol.Value(protocol.FieldNumber, 0)},
		}}}
	view := ui.NewTester(func(c *ui.Context) { m.connectorTaskHeader(c, &task, true) }, 680, 580)
	for _, label := range []string{"Task Reporting", "tier-2", "false", "0", m.connectionName(task.ConnectionID)} {
		if !view.HasText(label) {
			t.Fatalf("missing header text %q in %v", label, view.Texts())
		}
	}
	if detail.connectorDetail != nil {
		t.Fatal("rendering the header changed the selected detail segment")
	}
}

func TestConnectorScopeLegacyTypeAndExactDraft(t *testing.T) {
	var saved common.WorkbenchConnectorScopeEdit
	if err := decodeScopeEditJSON([]byte(`{"scope_id":"scope","revision":1,"boundary":{"scope_ref":"old-alpha","label":"Alpha","config":{"target":"alpha","counter":9007199254740993,"filter":"all"}},"scope_schema":{"version":1,"types":["space"],"schema":{"type":"object","required":["target"],"properties":{"target":{"type":"string"},"counter":{"type":"integer","enum":[9007199254740993]},"filter":{"type":"string"}}},"fields":{"target":{"title":"Target","control":"select","dynamic_options":true}}}}`), &saved); err != nil {
		t.Fatal(err)
	}
	plugin := services.WorkbenchPlugin{ScopeConfig: &saved.ConfigSchema, EndpointConfig: &protocol.ConfigSchema{Schema: json.RawMessage(`{"type":"object"}`)}}
	form := buildConnectorConfigForm(plugin, true)
	form.scope = saved.Boundary.Config
	panel := &connectorLoginPanel{edit: &connectorScopeEditor{id: "scope", saved: saved}, form: form, reuseEndpoint: true,
		scopeType: editableScopeType(saved), scopeTypes: saved.ConfigSchema.Types, scopeConfig: &saved.ConfigSchema}
	panel.activatePrimary()
	if input, problem := panel.optionInput(""); problem != "" || input["scope_type"] != "space" {
		t.Fatalf("legacy options: %v %s", input, problem)
	}
	input, valid := panel.scopeEditDraft()
	input.Config["filter"] = "beta-only"
	wire, err := json.Marshal(input)
	if !valid || err != nil || !bytes.Contains(wire, []byte(`"counter":9007199254740993`)) || saved.Boundary.ScopeType != "" || saved.Boundary.Config["filter"] != "all" {
		t.Fatalf("legacy edit draft changed saved boundary or lost precision: %s", wire)
	}
	if configFieldText(form.scope["counter"]) != "9007199254740993" ||
		panel.parseDraftField("integer", "9007199254740993") != form.scope["counter"] ||
		panel.parseDraftField("number", "0.1234567890123456789") != json.Number("0.1234567890123456789") {
		t.Fatal("scope form failed to preserve exact numeric values")
	}
	for _, field := range form.fields {
		if field.key == "counter" && field.choices[0] != form.scope["counter"] {
			t.Fatal("numeric enum and the saved value use different representations")
		}
	}
	var preview common.WorkbenchConnectorScopePreview
	if err := decodeScopeEditJSON([]byte(`{"before":{"scope_ref":"old-alpha","config":{"counter":9007199254740993}},"after":{"scope_type":"space","config":{"counter":9007199254740993}},"preview_token":"fresh"}`), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Before.ScopeType != "" || preview.After.ScopeType != "space" ||
		configFieldText(preview.After.Config["counter"]) != "9007199254740993" {
		t.Fatalf("legacy preview lost original type or exact number: %+v", preview)
	}
	saved.ConfigSchema.Types = []string{"project", "space"}
	if editableScopeType(saved) != "" {
		t.Fatal("a legacy scope with multiple permitted types requires an explicit choice")
	}
}

func TestConnectorScopeStaleKeepsDraftForReload(t *testing.T) {
	oldLocale := englishUI.Swap(false)
	t.Cleanup(func() { englishUI.Store(oldLocale) })
	initial := common.WorkbenchConnectorScopeEdit{Revision: 1, Boundary: common.WorkbenchConnectorBoundary{
		Config: map[string]any{"target": "alpha", "filter": "all", "counter": json.Number("9007199254740993")},
	}}
	latest := initial
	latest.Revision = 2
	latest.Boundary.Config = map[string]any{"target": "beta", "filter": "all", "counter": json.Number("9007199254740993")}
	draft := map[string]any{"target": "alpha", "filter": "beta-only", "counter": json.Number("9007199254740993")}
	panel := &connectorLoginPanel{form: &connectorConfigForm{scope: draft}, edit: &connectorScopeEditor{
		saved: initial, preview: &common.WorkbenchConnectorScopePreview{PreviewToken: "stale"},
	}}
	panel.scopeEditError(common.Err("STALE_REVISION", "Scope changed"))
	m := &mockup{connectorLogin: panel}
	view := ui.NewTester(m.connectorLoginDialog, 680, 580)
	if panel.edit.preview != nil || !view.HasText("重新读取范围") || view.HasText("确认并保存") {
		t.Fatalf("stale dialog should require reload before re-preview: %v", view.Texts())
	}
	rebaseScopeConfig(latest.Boundary.Config, draft, initial, latest, true)
	if latest.Boundary.Config["target"] != "beta" || latest.Boundary.Config["filter"] != "beta-only" ||
		latest.Boundary.Config["counter"] != json.Number("9007199254740993") {
		t.Fatalf("reload did not preserve compatible changes over the competing revision: %v", latest.Boundary.Config)
	}
}

func TestConnectorTaskHeaderShowsStatusOnlyWhilePending(t *testing.T) {
	m := connectorUIFixture()
	m.live = &services.Remote{}
	task := m.objects[0]
	state := &connectorTaskDetailState{version: task.TargetVersion, scopeID: task.ConnectionID, connectionVersion: task.ConnectionVersion,
		segment: "top_fields", loading: true}
	m.taskDetailUI(task.ID).connectorHeader = state
	render := func(c *ui.Context) { m.connectorTaskHeader(c, &task, true) }
	if view := ui.NewTester(render, 680, 580); !view.HasText(tr("正在读取关键来源字段")) {
		t.Fatal("pending header lacks a loading state")
	}
	state.loading, state.loaded = false, true
	if view := ui.NewTester(render, 680, 580); len(view.Texts()) != 0 {
		t.Fatalf("empty header has leftovers: %v", view.Texts())
	}
}
