package workbench

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/services"
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
