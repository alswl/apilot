package workbench

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestConnectorConfigFormDependenciesAndSelection(t *testing.T) {
	plugin := services.WorkbenchPlugin{
		EndpointConfig: &protocol.ConfigSchema{Version: 2, Schema: json.RawMessage(`{"type":"object","properties":{"cluster":{"type":"string","default":"demo"},"region":{"type":"string"}},"required":["cluster","region"]}`),
			Fields: map[string]protocol.ConfigField{"region": {DynamicOptions: true, DependsOn: []string{"cluster"}}}},
		ScopeConfig: &protocol.ConfigSchema{Version: 3, Schema: json.RawMessage(`{"type":"object","properties":{"board":{"type":"string"},"target":{"type":"string"}},"required":["board","target"]}`),
			Fields: map[string]protocol.ConfigField{"board": {DependsOn: []string{"endpoint.region"}}, "target": {DynamicOptions: true, DependsOn: []string{"board"}}}},
	}
	form := newConnectorConfigForm(plugin)
	if form == nil || form.primary != "target" {
		t.Fatalf("config form: %+v", form)
	}
	panel := &connectorLoginPanel{form: form, scopeType: protocol.ScopeProject, scopeConfig: plugin.ScopeConfig,
		endpointSchemaVersion: plugin.EndpointConfig.Version, selected: map[string]bool{"alpha": true, "beta": true}, fieldDrafts: map[string]string{}}
	panel.activatePrimary()
	if panel.optionField != "" || panel.canSave() {
		t.Fatal("incomplete dependencies enabled scope selection")
	}
	for _, field := range form.fields {
		switch field.key {
		case "region":
			if !form.ready(field) {
				t.Fatal("endpoint default did not satisfy dependency")
			}
			panel.updateDraft(field, "east")
		case "board":
			if !form.ready(field) {
				t.Fatal("endpoint choice did not satisfy scope dependency")
			}
			panel.updateDraft(field, "delivery")
		}
	}
	panel.activatePrimary()
	if panel.optionField != "target" || !panel.canSave() {
		t.Fatal("completed draft did not enable target selection")
	}
	panel.search = "beta"
	panel.choices = []protocol.ConfigOption{{Value: "beta"}}
	input := panel.saveInput()
	if !reflect.DeepEqual(input.EndpointConfig, map[string]any{"cluster": "demo", "region": "east"}) || len(input.Scopes) != 2 ||
		!reflect.DeepEqual(input.Scopes[0].Config, map[string]any{"target": "alpha", "board": "delivery"}) || input.ScopeSchemaVersion != 3 {
		t.Fatalf("configuration lost across search: %+v", input)
	}
	panel.setScopeType(protocol.ScopeSpace)
	if len(panel.selected) != 0 || len(form.scope) != 0 || form.endpoint["region"] != "east" {
		t.Fatal("type change did not reset only the scope")
	}
}

func TestConnectorConfigFormUnsupportedSchema(t *testing.T) {
	plugin := services.WorkbenchPlugin{EndpointConfig: &protocol.ConfigSchema{Schema: json.RawMessage(`{"type":"object"}`)},
		ScopeConfig: &protocol.ConfigSchema{Schema: json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object"}}}`)}}
	if form := newConnectorConfigForm(plugin); form != nil {
		t.Fatalf("unsupported nested form offered: %+v", form)
	}
}

func TestConnectorConfigJSONFallback(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"nested":{"type":"object"},"labels":{"type":"array"},"target":{"type":"string"}}}`,
		`{"type":"object","oneOf":[{"required":["target"]},{"required":["ref"]}]}`,
	} {
		plugin := services.WorkbenchPlugin{Enabled: true, Imported: true, Release: &common.WorkbenchConnectorRelease{ConnectorID: "complex"}, EndpointSchemaVersion: 2,
			EndpointConfig: &protocol.ConfigSchema{Version: 2, Schema: json.RawMessage(`{"type":"object"}`)},
			ScopeConfig:    &protocol.ConfigSchema{Version: 1, Schema: json.RawMessage(schema), Types: []string{"space", "project"}},
		}
		if !connectorCanAdd(plugin) {
			t.Fatal("enabled imported Connector is hidden")
		}
		m := &mockup{livePlugins: []services.WorkbenchPlugin{plugin}}
		m.openConnectorLogin("complex", "")
		panel := m.connectorLogin
		if panel.form != nil || panel.jsonDraft == nil || panel.scopeType != protocol.ScopeProject {
			t.Fatalf("JSON fallback missing: %+v", panel)
		}
		panel.jsonDraft.endpoint = `{"nested":{"ports":[1,9007199254740993]}}`
		panel.jsonDraft.scope = `{"labels":["a","b"],"nested":{"enabled":false}}`
		panel.jsonDraft.choose("scope", "target", "alpha")
		if !panel.canSave() {
			t.Fatal("valid objects cannot be submitted")
		}
		input := panel.saveInput()
		endpoint, _ := json.Marshal(input.EndpointConfig)
		scope, _ := json.Marshal(input.Scopes[0].Config)
		if string(endpoint) != `{"nested":{"ports":[1,9007199254740993]}}` || string(scope) != `{"labels":["a","b"],"nested":{"enabled":false},"target":"alpha"}` {
			t.Fatalf("configuration lost: %s %s", endpoint, scope)
		}
		panel.setScopeType(protocol.ScopeSpace)
		if panel.jsonDraft.scope != "{}" || panel.scopeType != "space" || panel.jsonDraft.endpoint == "{}" {
			t.Fatal("scope switch lost endpoint or preserved the old scope")
		}
		panel.jsonDraft.scope = `[]`
		if panel.canSave() || len(panel.saveInput().Scopes) != 0 {
			t.Fatal("non-object config could be submitted")
		}
	}
}

func TestConnectorConfigJSONRejectsDeclaredCredentials(t *testing.T) {
	d := &connectorJSONDraft{endpoint: `{"settings":{"token":"example"}}`, scope: "{}",
		endpointSchema: &protocol.ConfigSchema{Schema: json.RawMessage(`{"type":"object","properties":{"settings":{"type":"object","properties":{"token":{"type":"string","writeOnly":true}}}}}`)},
	}
	if _, _, ok := d.objects(); ok {
		t.Fatal("declared credential could be submitted")
	}
}

func TestConnectorConfigEnumInLoginModal(t *testing.T) {
	panel := &connectorLoginPanel{
		auth:     common.WorkbenchConnectorAuthorization{Status: "authorized"},
		selected: map[string]bool{}, connected: map[string]bool{},
		form: &connectorConfigForm{endpoint: map[string]any{}, scope: map[string]any{}, fields: []connectorConfigField{
			{key: "cluster", kind: "string", level: "endpoint", required: true, choices: []any{"demo"}, meta: protocol.ConfigField{Title: "Cluster"}},
			{key: "region", kind: "string", level: "endpoint", required: true, meta: protocol.ConfigField{Title: "Region", DynamicOptions: true, DependsOn: []string{"cluster"}}},
			{key: "board", kind: "string", level: "scope", required: true, choices: []any{"delivery"}, meta: protocol.ConfigField{Title: "Board", DependsOn: []string{"endpoint.region"}}},
		}},
	}
	m := &mockup{connectorLogin: panel}
	view := ui.NewTester(m.connectorLoginDialog, 640, 480)
	click(t, view, "demo")
	if panel.form.endpoint["cluster"] != "demo" || !panel.form.ready(panel.form.fields[1]) {
		t.Fatal("clicking the visible option did not enable Region")
	}
}

func TestConnectorJSONEditorsFocusAndInput(t *testing.T) {
	panel := &connectorLoginPanel{jsonDraft: &connectorJSONDraft{endpoint: "{}", scope: "{}"}}
	m := &mockup{connectorLogin: panel}
	view := ui.NewTester(func(c *ui.Context) { m.connectorJSONFields(c, panel) }, 640, 480)
	click(t, view, "端点配置 JSON")
	if !view.Focused("端点配置 JSON") || view.Focused("范围配置 JSON") {
		t.Fatal("endpoint editor did not get exclusive focus")
	}
	view.Key(ui.Cmd, ui.KeyA)
	view.Type(`{"cluster":"demo","region":"east"}`)
	click(t, view, "范围配置 JSON")
	if view.Focused("端点配置 JSON") || !view.Focused("范围配置 JSON") {
		t.Fatal("scope editor did not get exclusive focus")
	}
	view.Key(ui.Cmd, ui.KeyA)
	view.Type(`{"target":"alpha","metadata":{"counter":9007199254740993}}`)
	input := panel.saveInput()
	if len(input.Scopes) != 1 || input.EndpointConfig["cluster"] != "demo" {
		t.Fatalf("drafts failed to preserve separate objects: %q / %q", panel.jsonDraft.endpoint, panel.jsonDraft.scope)
	}
	metadata, _ := input.Scopes[0].Config["metadata"].(map[string]any)
	if counter := metadata["counter"]; counter != json.Number("9007199254740993") {
		t.Fatalf("counter = %v", counter)
	}
}

func TestConnectorSavedEndpointBypassesNewRequiredFields(t *testing.T) {
	form := newConnectorConfigForm(services.WorkbenchPlugin{
		EndpointConfig: &protocol.ConfigSchema{Version: 2, Schema: json.RawMessage(`{"type":"object","properties":{"region":{"type":"string"}},"required":["region"]}`)},
		ScopeConfig: &protocol.ConfigSchema{Version: 1, Schema: json.RawMessage(`{"type":"object","properties":{"target":{"type":"string"}},"required":["target"]}`),
			Fields: map[string]protocol.ConfigField{"target": {DynamicOptions: true, DependsOn: []string{"endpoint.region"}}}},
	})
	panel := &connectorLoginPanel{form: form, scopeType: "space", endpointSchemaVersion: 2, selected: map[string]bool{"alpha": true}}
	if form == nil || panel.canSave() {
		t.Fatal("new required endpoint field was not enforced")
	}
	panel.setReuseEndpoint(true)
	if panel.field() != "target" || !panel.canSave() {
		t.Fatal("saved endpoint did not satisfy form")
	}
	input, problem := panel.optionInput("")
	if problem != "" || input["reuse_saved_endpoint"] != true || input["endpoint_config"] != nil {
		t.Fatalf("option input: %v %s", input, problem)
	}
	save := panel.saveInput()
	if !save.ReuseSavedEndpoint || save.EndpointConfig != nil || save.Scopes[0].ScopeType != "space" {
		t.Fatalf("save input: %+v", save)
	}
}

func TestConnectorRetryPreservesDraftAndSkipsSavedScopes(t *testing.T) {
	panel := &connectorLoginPanel{scopeType: "project", kind: managedFixture,
		selected: map[string]bool{"alpha": true, "beta": true}, connected: map[string]bool{},
	}
	panel.applyScopeResults(panel.saveInput(), []common.WorkbenchConnectorScopeResult{
		{ScopeID: "alpha-id", Status: "queued"}, {Status: "rejected", ErrorKind: "invalid_config"},
	})
	if panel.finished() || !panel.canSave() || len(panel.saveInput().Scopes) != 1 || panel.saveInput().Scopes[0].Config["project"] != "beta" {
		t.Fatalf("incorrect retry batch: %+v", panel.saveInput())
	}
	panel.selected["alpha"] = true
	if len(panel.saveInput().Scopes) != 1 {
		t.Fatal("a saved scope was included again")
	}
	panel.applyScopeResults(panel.saveInput(), []common.WorkbenchConnectorScopeResult{{ScopeID: "beta-id", Status: "queued"}})
	if !panel.finished() || len(panel.results) != 2 {
		t.Fatal("saved results were not preserved")
	}

	panel = &connectorLoginPanel{scopeType: "space", jsonDraft: &connectorJSONDraft{endpoint: `{}`, scope: `{"target":"bad"}`}}
	panel.applyScopeResults(panel.saveInput(), []common.WorkbenchConnectorScopeResult{{Status: "rejected", ErrorKind: "invalid_config"}})
	if panel.finished() || !panel.canSave() || panel.jsonDraft.scope != `{"target":"bad"}` {
		t.Fatal("rejected JSON draft was lost")
	}
	panel.jsonDraft.scope = `{"target":"alpha"}`
	if panel.saveInput().Scopes[0].Config["target"] != "alpha" {
		t.Fatal("edited draft was not resubmitted")
	}
}

func TestConnectorOptionLimitAndPasswordFormat(t *testing.T) {
	panel := &connectorLoginPanel{scopeType: "space", optionLevel: "scope", jsonDraft: &connectorJSONDraft{endpoint: `{}`,
		scope: `{"value":"` + strings.Repeat("界", common.WorkbenchConnectorOptionsConfigLimit-12) + `"}`}}
	if _, problem := panel.optionInput(""); problem != "" {
		t.Fatalf("limit counts characters, not bytes: %s", problem)
	}
	panel.jsonDraft.scope = `{"value":"` + strings.Repeat("界", common.WorkbenchConnectorOptionsConfigLimit) + `"}`
	if _, problem := panel.optionInput(""); problem == "" || !panel.canSave() {
		t.Fatal("option-specific limit was not enforced")
	}
	password := &protocol.ConfigSchema{Version: 1, Schema: json.RawMessage(`{"type":"object","properties":{"token":{"type":"string","format":"password"}}}`)}
	if newConnectorConfigForm(services.WorkbenchPlugin{EndpointConfig: password, ScopeConfig: password}) != nil {
		t.Fatal("password exposed as an ordinary field")
	}
}

func TestConnectorOpaqueNextOptionDoesNotSharePagingState(t *testing.T) {
	for _, mode := range []string{"scope selection", "dynamic field", "JSON draft"} {
		t.Run(mode, func(t *testing.T) {
			panel := &connectorLoginPanel{
				auth: common.WorkbenchConnectorAuthorization{Status: "authorized"}, scopeType: "project",
				optionLevel: "scope", optionField: "target", next: "next-page-token",
				choices:  []protocol.ConfigOption{{Value: "next", Label: "Opaque option"}},
				selected: map[string]bool{}, connected: map[string]bool{},
			}
			switch mode {
			case "scope selection":
				panel.form = &connectorConfigForm{primary: "target", endpoint: map[string]any{}, scope: map[string]any{}}
			case "dynamic field":
				panel.form = &connectorConfigForm{fields: []connectorConfigField{{key: "target", level: "scope", kind: "string", meta: protocol.ConfigField{DynamicOptions: true}}},
					endpoint: map[string]any{}, scope: map[string]any{}}
			case "JSON draft":
				panel.jsonDraft = &connectorJSONDraft{endpoint: "{}", scope: "{}"}
			}
			m := &mockup{connectorLogin: panel}
			view := ui.NewTester(m.connectorLoginDialog, 1000, 900)
			if !view.HasText(tr("加载更多")) {
				t.Fatal("paging button not rendered")
			}
			click(t, view, "Opaque option")
			switch mode {
			case "scope selection":
				if !panel.selected["next"] || panel.next != "next-page-token" {
					t.Fatal("checkbox did not select the opaque value")
				}
			case "dynamic field":
				if panel.form.scope["target"] != "next" {
					t.Fatalf("dynamic draft = %v", panel.form.scope)
				}
			case "JSON draft":
				if _, scope, ok := panel.jsonDraft.objects(); !ok || scope["target"] != "next" {
					t.Fatalf("JSON draft = %s", panel.jsonDraft.scope)
				}
			}
			if panel.busy {
				t.Fatal("selecting an option started paging")
			}
		})
	}
}
