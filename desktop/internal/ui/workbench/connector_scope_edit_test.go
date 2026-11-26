package workbench

import (
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
	click(t, view, "返回编辑")
	draft, valid := panel.scopeEditDraft()
	if !valid || panel.edit.preview != nil || draft.Config["filter"] != "beta-only" || draft.Config["target"] != "beta" {
		t.Fatalf("confirmation discarded draft: %+v", draft)
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
