package workbench

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorScopeEditor struct {
	id, label, problem string
	saved              common.WorkbenchConnectorScopeEdit
	draft              common.WorkbenchConnectorScopeDraft
	preview            *common.WorkbenchConnectorScopePreview
	legacyOptions      bool
	optionLabels       map[[2]string]string
}

func (m *mockup) openConnectorScopeEdit(scope common.WorkbenchConnectorScopeOverview) {
	p := &connectorLoginPanel{edit: &connectorScopeEditor{id: scope.ScopeID, label: scope.Label, optionLabels: map[[2]string]string{}}, busy: true,
		selected: map[string]bool{}, connected: map[string]bool{}, fieldDrafts: map[string]string{}, reuseEndpoint: true}
	m.connectorLogin = p
	m.readConnectorScopeEdit(p)
}

func (m *mockup) readConnectorScopeEdit(p *connectorLoginPanel) {
	edit := p.edit
	p.busy, edit.problem = true, ""
	connectorUIRequest(m, "workbench.connector.scope.show", map[string]string{"id": edit.id}, func(value common.WorkbenchConnectorScopeEdit, err error) {
		if m.connectorLogin != p || (err == nil && value.ScopeID != edit.id) {
			return
		}
		p.busy = false
		if err != nil {
			edit.problem = common.ProtocolError(err).Message
			return
		}
		edit.saved = value
		p.scopeConfig = &edit.saved.ConfigSchema
		p.scopeType, p.scopeTypes = value.Boundary.ScopeType, value.ConfigSchema.Types
		plugin := services.WorkbenchPlugin{ScopeConfig: p.scopeConfig}
		if conn := m.connectionByID(edit.id); conn != nil {
			for _, current := range m.livePlugins {
				if current.ID == conn.SourceKind {
					plugin.EndpointConfig = current.EndpointConfig
					edit.legacyOptions = current.Release != nil && !current.Imported && current.ID != managedGitHub
					break
				}
			}
		}
		if plugin.EndpointConfig == nil {
			plugin.EndpointConfig = &protocol.ConfigSchema{Schema: json.RawMessage(`{"type":"object"}`)}
		}
		p.form = newConnectorConfigForm(plugin)
		if p.form == nil {
			p.jsonDraft = newConnectorJSONDraft(plugin)
			raw, _ := json.MarshalIndent(value.Boundary.Config, "", "  ")
			p.jsonDraft.scope = string(raw)
		} else {
			p.form.reuseEndpoint = true
			p.form.scope = maps.Clone(value.Boundary.Config)
			if p.form.scope == nil {
				p.form.scope = map[string]any{}
			}
			p.activatePrimary()
		}
		if p.field() != "" {
			m.loadConnectorChoices(p, "")
		}
	})
}

func (p *connectorLoginPanel) scopeEditDraft() (common.WorkbenchConnectorScopeDraft, bool) {
	draft := common.WorkbenchConnectorScopeDraft{Revision: p.edit.saved.Revision, ScopeType: p.scopeType, SchemaVersion: p.edit.saved.ConfigSchema.Version}
	if draft.Revision == 0 || len(p.scopeTypes) != 0 && !slices.Contains(p.scopeTypes, p.scopeType) {
		return draft, false
	}
	if p.jsonDraft != nil {
		_, config, valid := p.jsonDraft.config(true)
		draft.Config = config
		return draft, valid
	}
	if p.form == nil || !p.form.complete() {
		return draft, false
	}
	if p.form.primary != "" && p.form.scope[p.form.primary] == nil {
		return draft, false
	}
	for _, field := range p.form.fields {
		if text := p.fieldDrafts[field.level+"."+field.key]; strings.TrimSpace(text) != "" && parseConfigField(field.kind, text) == nil {
			return draft, false
		}
	}
	draft.Config = maps.Clone(p.form.scope)
	return draft, true
}

func connectorDraftTarget(c *ui.Context, p *connectorLoginPanel) {
	if p.form == nil || p.form.primary == "" {
		return
	}
	field := p.form.primary
	value, ok := p.form.scope[field].(string)
	if !ok || value == "" || (p.field() == field && slices.ContainsFunc(p.choices, func(choice protocol.ConfigOption) bool {
		return choice.Value == value && !p.connected[value]
	})) {
		return
	}
	label := p.edit.optionLabels[[2]string{field, value}]
	if label == "" {
		label = value
	}
	title := p.scopeConfig.Fields[field].Title
	if title == "" {
		title = field
	}
	components.PropertyGrid(c, []components.Property{{ID: field, Title: title, Value: label}}, 1)
}

func (m *mockup) connectorEditOption(c *ui.Context, p *connectorLoginPanel, choice protocol.ConfigOption) {
	if p.connected[choice.Value] {
		components.Muted(c, choice.Label+" · "+tr("已连接"))
		return
	}
	if p.jsonDraft != nil {
		if ui.Button(c, choice.Label).Disabled(p.busy).Clicked() {
			p.jsonDraft.choose(p.optionLevel, p.field(), choice.Value, true)
		}
		return
	}
	for _, field := range p.form.fields {
		if field.level != "scope" || field.key != p.field() {
			continue
		}
		selected := p.form.scope[field.key] == choice.Value
		if ui.Checkbox(c, &selected, choice.Label).Disabled(p.busy).Changed() {
			if selected {
				p.updateDraft(field, choice.Value)
			} else {
				p.updateDraft(field, nil)
			}
		}
		return
	}
}

func (m *mockup) previewConnectorScopeEdit(p *connectorLoginPanel, draft common.WorkbenchConnectorScopeDraft) {
	p.saving, p.edit.problem = true, ""
	p.edit.draft = draft
	input := map[string]any{"id": p.edit.id, "body": draft}
	connectorUIRequest(m, "workbench.connector.scope.preview", input, func(value common.WorkbenchConnectorScopePreview, err error) {
		if m.connectorLogin != p {
			return
		}
		p.saving = false
		if err != nil {
			p.edit.problem = common.ProtocolError(err).Message
			return
		}
		p.edit.preview = &value
	})
}

func (m *mockup) confirmConnectorScopeEdit(p *connectorLoginPanel) {
	edit := p.edit
	if p.saving || edit.preview == nil || edit.preview.PreviewToken == "" {
		return
	}
	p.saving, edit.problem = true, ""
	input := map[string]any{"id": edit.id, "body": common.WorkbenchConnectorScopeUpdate{
		WorkbenchConnectorScopeDraft: edit.draft, PreviewToken: edit.preview.PreviewToken,
	}}
	connectorUIRequest(m, "workbench.connector.scope.save", input, func(value common.WorkbenchConnectorScopeUpdateResult, err error) {
		if m.connectorLogin != p {
			return
		}
		p.saving = false
		if err != nil {
			edit.problem = common.ProtocolError(err).Message
			return
		}
		m.connectorLogin = nil
		m.connectorScopeFocus = edit.id
		m.connectorProblem = ""
		if value.SyncStatus == "sync_not_started" {
			m.connectorProblem = tr("范围已保存。请立即同步。")
		}
		m.refreshLive()
	})
}

func (m *mockup) connectorScopeEditDialog(c *ui.Context, p *connectorLoginPanel) {
	open := true
	width, height := c.Size()
	edit := p.edit
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(max(280, min(width-64, 480))).Gap(12).Children(func() {
			if edit.preview == nil {
				components.Title(c.Key("title"), tr("配置同步范围"))
				components.Muted(c.Key("scope"), edit.label)
				if edit.saved.Revision != 0 {
					if len(p.scopeTypes) == 1 {
						components.Muted(c.Key("type"), scopeTypeLabel(p.scopeType))
					}
					m.connectorScopeForm(c, p, height)
				} else if p.busy {
					components.StatusLabel(c.Key("loading"), components.Status{Text: tr("正在读取范围配置"), Loading: true, Color: c.Theme().TextMuted})
				}
			} else {
				components.Title(c.Key("title"), tr("确认同步范围"))
				ui.Scroll(c.Key("boundaries")).FillWidth().MaxHeight(max(160, min(height-220, 330))).Children(func() {
					ui.Column(c.Key("changes")).FillWidth().Gap(16).Children(func() {
						connectorEditBoundary(c.Key("before"), tr("当前范围"), edit.preview.Before, edit.saved.ConfigSchema)
						connectorEditBoundary(c.Key("after"), tr("新范围"), edit.preview.After, edit.saved.ConfigSchema)
					})
				})
			}
			if p.problem != "" && p.problem != p.searchLengthProblem() && edit.preview == nil {
				components.Muted(c.Key("options-error"), p.problem)
			}
			if edit.problem != "" {
				components.Muted(c.Key("problem"), edit.problem)
			}
			ui.Row(c.Key("buttons")).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if edit.preview != nil {
					if ui.Button(c.Key("back"), tr("返回编辑")).Disabled(p.saving).Clicked() {
						edit.preview, edit.problem = nil, ""
					}
					if ui.PrimaryButton(c.Key("confirm"), tr("确认并保存")).Disabled(p.saving).Clicked() {
						m.confirmConnectorScopeEdit(p)
					}
					return
				}
				if ui.Button(c.Key("cancel"), tr("取消")).Disabled(p.saving).Clicked() {
					open = false
				}
				if edit.saved.Revision == 0 && !p.busy {
					if ui.PrimaryButton(c.Key("retry"), tr("重试")).Clicked() {
						m.readConnectorScopeEdit(p)
					}
				} else if draft, valid := p.scopeEditDraft(); ui.PrimaryButton(c.Key("preview"), tr("预览更改")).Disabled(!valid || p.busy || p.saving).Clicked() {
					m.previewConnectorScopeEdit(p, draft)
				}
			})
		})
	})
	if !open && !p.saving {
		if edit.preview != nil {
			edit.preview, edit.problem = nil, ""
		} else {
			if p.cancelQuery != nil {
				p.cancelQuery()
			}
			m.connectorLogin = nil
			m.connectorScopeFocus = edit.id
		}
	}
}

func scopeTypeLabel(value string) string {
	switch value {
	case protocol.ScopeProject:
		return tr("项目")
	case protocol.ScopeSpace:
		return tr("空间")
	default:
		return value
	}
}

func connectorEditBoundary(c *ui.Context, title string, boundary common.WorkbenchConnectorBoundary, schema protocol.ConfigSchema) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		components.Heading(c.Key("title"), title)
		label := boundary.Label
		if kind := scopeTypeLabel(boundary.ScopeType); kind != "" {
			label += " · " + kind
		}
		components.Body(c.Key("boundary"), label)
		for _, key := range slices.Sorted(maps.Keys(boundary.Config)) {
			name := schema.Fields[key].Title
			if name == "" {
				name = key
			}
			components.PropertyGrid(c.Key(key), []components.Property{{ID: key, Title: name, Value: configFieldText(boundary.Config[key])}}, 1)
		}
	})
}
