package workbench

import (
	"fmt"
	"maps"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorMappingPanel struct {
	scope, task      string
	catalog          protocol.FieldCatalog
	draft            map[string]protocol.MappingRule
	value            common.WorkbenchMappingResult
	restore, loading bool
	problem          string
}

var projectionFields = []struct {
	id, label string
	kind      protocol.FieldType
}{
	{"title", "任务名称", protocol.FieldText}, {"description", "描述", protocol.FieldRichText},
	{"assignees", "负责人", protocol.FieldAccounts}, {"priority", "优先级", protocol.FieldNumber},
	{"due_date", "截止日", protocol.FieldDate}, {"labels", "来源标签", protocol.FieldLabels}, {"state", "状态", protocol.FieldText},
}

func fieldCaption(id string) string {
	if id == "state_reason" {
		return tr("关闭原因")
	}
	for _, field := range projectionFields {
		if field.id == id {
			return tr(field.label)
		}
	}
	return tr(id)
}

func (m *mockup) openConnectorMapping(scope, task string) {
	p := &connectorMappingPanel{scope: scope, task: task, loading: true}
	m.connectorMapping = p
	input := map[string]string{"id": scope}
	connectorUIRequest(m, "workbench.scope.fields", input, func(catalog protocol.FieldCatalog, err error) {
		if m.connectorMapping != p {
			return
		}
		if err != nil {
			p.loading = false
			p.problem = common.ProtocolError(err).Message
			return
		}
		p.catalog = catalog
		connectorUIRequest(m, "workbench.scope.mapping", input, func(result common.WorkbenchMappingResult, err error) {
			if m.connectorMapping != p {
				return
			}
			p.loading = false
			if err != nil {
				p.problem = common.ProtocolError(err).Message
				return
			}
			p.value = result
			p.draft = maps.Clone(result.Rules)
			if p.draft == nil {
				p.draft = map[string]protocol.MappingRule{}
			}
		})
	})
}

func (m *mockup) submitConnectorMapping(p *connectorMappingPanel, save bool) {
	p.loading, p.problem = true, ""
	input := common.WorkbenchMappingInput{Revision: p.value.Revision, TaskID: p.task, RestoreDefaults: p.restore}
	if !p.restore {
		input.Rules = maps.Clone(p.draft)
	}
	action := "preview"
	if save {
		action = "save"
	}
	connectorUIRequest(m, "workbench.scope.mapping."+action, struct {
		ID   string                       `json:"id"`
		Body common.WorkbenchMappingInput `json:"body"`
	}{p.scope, input}, func(value common.WorkbenchMappingResult, err error) {
		if m.connectorMapping != p {
			return
		}
		p.loading = false
		if err != nil {
			p.problem = common.ProtocolError(err).Message
			return
		}
		p.value = value
		if save && len(value.Issues) == 0 {
			if value.PendingRead {
				m.connectorProblem = tr("等待同步更新映射")
			}
			m.connectorMapping = nil
			m.refreshLive()
		}
	})
}

func (m *mockup) connectorMappingDialog(c *ui.Context) {
	p := m.connectorMapping
	if p == nil {
		return
	}
	open := true
	_, height := c.Size()
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(500).Gap(12).Children(func() {
			components.Title(c.Key("title"), tr("字段映射"))
			components.Muted(c.Key("scope"), m.connectionName(p.scope))
			if p.draft != nil {
				ui.Scroll(c.Key("fields")).MaxHeight(max(140, min(height-260, 390))).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						for _, field := range projectionFields {
							rule := p.draft[field.id]
							options := map[string]string{}
							names := map[string]protocol.FieldDefinition{}
							selected := ""
							for _, native := range p.catalog.Fields {
								if native.Type != field.kind {
									continue
								}
								label := native.Label + " · " + native.ID
								options[label] = native.ID
								names[native.ID] = native
								if rule.Field == native.ID {
									selected = label
								}
							}
							if len(options) == 0 {
								continue
							}
							if selected == "" {
								selected = tr("选择来源字段")
								if rule.Field != "" {
									selected = rule.Field
								}
								options[selected] = rule.Field
							}
							ui.Row(c.Key(field.id)).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
								components.DataText(c.Key("label"), tr(field.label))
								old := selected
								ui.Select(c.Key("field"), &selected, sortedConnectorKeys(options)).MinWidth(170).Disabled(p.loading || p.restore)
								if selected != old {
									rule.Field = options[selected]
									if !names[rule.Field].Writable {
										rule.Write = false
									}
									p.draft[field.id] = rule
								}
								if field.id != "state" && names[rule.Field].Writable {
									if ui.Checkbox(c.Key("write"), &rule.Write, tr("允许回写")).Disabled(p.loading || p.restore).Changed() {
										p.draft[field.id] = rule
									}
								}
							})
							if field.id == "state" && len(rule.Values) > 0 {
								for _, id := range sortedConnectorKeys(rule.Values) {
									choices := map[string]string{"待处理": "open", "进行中": "in_progress", "已完成": "done", "不计划处理": "cancelled", "未知": "unknown"}
									chosen := "未知"
									for label, value := range choices {
										if rule.Values[id] == value {
											chosen = label
										}
									}
									ui.Row(c.Key("state-" + id)).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
										components.Muted(c.Key("native"), id)
										if !p.loading && !p.restore && usageChoice(c.Key("state"), &chosen, choices, "未知", 130) {
											rule.Values = maps.Clone(rule.Values)
											rule.Values[id] = choices[chosen]
											p.draft[field.id] = rule
										} else if p.loading || p.restore {
											components.DataText(c.Key("normalized"), tr(chosen))
										}
									})
								}
							}
							if issue := p.value.Issues[field.id]; issue != "" {
								components.Muted(c.Key(field.id+"-issue"), connectorStatus(issue))
							}
						}
					})
				})
				if p.value.Sample != nil {
					s := p.value.Sample
					components.Body(c.Key("preview"), s.Title+" · "+m.taskStateLabel(*s)+" · "+tr("截止日")+" "+taskDueValue(*s))
				}
				if p.value.PendingRead {
					components.Muted(c.Key("pending"), tr("等待同步更新映射"))
				}
			}
			if p.problem != "" {
				components.Muted(c.Key("problem"), p.problem)
			}
			if p.loading {
				components.Muted(c.Key("loading"), tr("正在读取来源详情"))
			}
			ui.Row(c.Key("footer")).FillWidth().Wrap().Justify(ui.End).Gap(8).Children(func() {
				ui.Checkbox(c.Key("restore"), &p.restore, tr("恢复默认映射")).Disabled(p.loading || p.draft == nil)
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					open = false
				}
				if ui.Button(c.Key("preview"), tr("预览")).Disabled(p.loading || p.draft == nil).Clicked() {
					m.submitConnectorMapping(p, false)
				}
				if ui.PrimaryButton(c.Key("save"), tr("保存配置")).Disabled(p.loading || p.draft == nil).Clicked() {
					m.submitConnectorMapping(p, true)
				}
			})
		})
	})
	if !open {
		m.connectorMapping = nil
	}
}

func (m *mockup) toggleConnectorScopeWrite(conn *connection, write bool) {
	id := conn.ID
	input := map[string]any{"id": id, "kind": conn.SourceKind, "name": conn.Name, "version": conn.Version, "write_enabled": write}
	connectorUIRequest[common.WorkbenchSnapshot](m, "workbench.connection.save", input, func(value common.WorkbenchSnapshot, err error) {
		m.connectorProblem = ""
		if err != nil {
			m.connectorProblem = fmt.Sprintf("%s · %s", conn.Name, common.ProtocolError(err).Message)
		}
		m.refreshLive()
	})
}
