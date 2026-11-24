package workbench

import (
	"encoding/json"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorTaskEditor struct {
	task                                       sourceObject
	actionMode                                 bool
	loading, saving                            bool
	fields                                     []protocol.FieldDefinition
	actions                                    []protocol.StateAction
	values, original, actionValues             map[string]string
	action, problem                            string
	repositoryLabels, originalRepositoryLabels []string
}

func (m *mockup) supportsConnectorEditor(task sourceObject) bool {
	if m.live == nil {
		return false
	}
	conn := m.taskConnection(task)
	if conn == nil || conn.Paused || conn.PluginDisabled {
		return false
	}
	return task.Kind == common.WorkbenchLocal || (isManagedConnector(task.Kind) && conn.WriteEnabled && conn.Status != "授权不可用")
}

func (m *mockup) openConnectorTaskEditor(task sourceObject, action bool) {
	if !m.supportsConnectorEditor(task) || m.sourceWritePending(task.ID) || m.serverTaskOperations[task.ID].SyncState == common.WorkbenchOperationNeedsReview {
		return
	}
	p := &connectorTaskEditor{task: task, actionMode: action, loading: true, values: map[string]string{}, original: map[string]string{}, actionValues: map[string]string{}}
	m.connectorEditor011 = p
	connectorUIRequest(m, "workbench.task.edit-metadata", map[string]string{"id": task.ID}, func(meta protocol.EditMetadataResult, err error) {
		if m.connectorEditor011 != p {
			return
		}
		p.loading = false
		if err != nil {
			p.problem = common.ProtocolError(err).Message
			return
		}
		p.fields, p.actions = meta.Fields, meta.Actions
		if task.Kind == managedGitHub {
			p.repositoryLabels = slices.Clone(task.Labels)
			var native protocol.FieldValue
			var names []string
			if json.Unmarshal([]byte(task.NativeFields["native.labels"]), &native) == nil && json.Unmarshal(native.Value, &names) == nil {
				p.repositoryLabels = names
			}
			p.originalRepositoryLabels = slices.Clone(p.repositoryLabels)
		}
		if len(p.actions) > 0 {
			p.action = p.actions[0].ID
		}
		for _, field := range p.fields {
			value := taskEditorValue(task, field.ID)
			p.values[field.ID], p.original[field.ID] = value, value
		}
		if !action && isManagedConnector(task.Kind) {
			for _, field := range p.fields {
				if field.ID == "description" {
					p.loading = true
					connectorUIRequest(m, "workbench.task.detail", map[string]string{"id": task.ID, "segment": "description"}, func(detail common.WorkbenchTaskDetail, err error) {
						if m.connectorEditor011 != p {
							return
						}
						p.loading = false
						if err != nil {
							p.problem = common.ProtocolError(err).Message
							p.fields = withoutDescription(p.fields)
							return
						}
						if len(detail.Items) > 0 && detail.Items[0].Text != nil {
							value := detailValue(protocol.FieldValue{Value: detail.Items[0].Text.Content})
							p.values["description"], p.original["description"] = value, value
						}
					})
					break
				}
			}
		}
	})
}

func withoutDescription(fields []protocol.FieldDefinition) []protocol.FieldDefinition {
	out := make([]protocol.FieldDefinition, 0, len(fields))
	for _, field := range fields {
		if field.ID != "description" {
			out = append(out, field)
		}
	}
	return out
}

func taskEditorValue(task sourceObject, id string) string {
	switch id {
	case "title":
		return task.Title
	case "description":
		return task.Summary
	case "assignees":
		return strings.Join(task.Assignees, ", ")
	case "priority":
		return taskPriorityValue(task)
	case "due_date":
		return taskDueValue(task)
	case "labels":
		return strings.Join(task.Labels, ", ")
	}
	return ""
}

func connectorEditorValue(field protocol.FieldDefinition, value string, task sourceObject) (protocol.FieldValue, bool) {
	trimmed := strings.TrimSpace(value)
	switch field.Type {
	case protocol.FieldText:
		if trimmed == "" {
			return protocol.FieldValue{}, false
		}
		return protocol.Value(field.Type, trimmed), true
	case protocol.FieldRichText:
		return protocol.Value(field.Type, value), true
	case protocol.FieldDate:
		if trimmed == "" {
			return protocol.Value(field.Type, nil), true
		}
		if _, err := time.Parse(time.DateOnly, trimmed); err != nil {
			return protocol.FieldValue{}, false
		}
		return protocol.Value(field.Type, trimmed), true
	case protocol.FieldNumber:
		if trimmed == "" {
			return protocol.Value(field.Type, nil), true
		}
		value, err := strconv.Atoi(trimmed)
		return protocol.Value(field.Type, value), err == nil
	case protocol.FieldLabels:
		return protocol.Value(field.Type, splitValues(trimmed)), true
	case protocol.FieldAccounts:
		options := append([]protocol.Option{}, field.Options...)
		var current protocol.FieldValue
		var accounts []protocol.Option
		if json.Unmarshal([]byte(task.NativeFields["native.assignees"]), &current) == nil && json.Unmarshal(current.Value, &accounts) == nil {
			options = append(options, accounts...)
		}
		values := []protocol.Option{}
		for _, name := range splitValues(trimmed) {
			var chosen protocol.Option
			for _, option := range options {
				if strings.EqualFold(option.Label, name) || strings.EqualFold(option.ID, name) {
					chosen = option
					break
				}
			}
			if chosen.ID == "" {
				chosen = protocol.Option{ID: name, Label: name}
			}
			values = append(values, chosen)
		}
		return protocol.Value(field.Type, values), true
	default:
		return protocol.FieldValue{}, false
	}
}

func (m *mockup) submitConnectorEditor(p *connectorTaskEditor) {
	task := p.task
	request := common.WorkbenchTaskWrite{RequestID: common.ID(), ConnectionID: task.ConnectionID, ConnectionVersion: task.ConnectionVersion, TargetVersion: task.TargetVersion, SourceObjectID: task.ID}
	if p.actionMode {
		request.Action = p.action
		request.ActionFields = map[string]protocol.FieldValue{}
		for _, action := range p.actions {
			if action.ID != p.action {
				continue
			}
			for _, field := range action.Required {
				value, ok := connectorEditorValue(field, p.actionValues[field.ID], task)
				if !ok || strings.TrimSpace(p.actionValues[field.ID]) == "" {
					p.problem = tr("填写操作所需字段") + " · " + connectorRequiredFieldLabel(field)
					return
				}
				request.ActionFields[field.ID] = value
			}
		}
		if request.Action == "" {
			return
		}
	} else {
		request.Fields = map[string]protocol.FieldValue{}
		for _, field := range p.fields {
			if p.usesRepositoryLabels(field) {
				if !slices.Equal(p.repositoryLabels, p.originalRepositoryLabels) {
					request.Fields[field.ID] = protocol.Value(field.Type, append([]string{}, p.repositoryLabels...))
				}
				continue
			}
			if p.values[field.ID] == p.original[field.ID] {
				continue
			}
			value, ok := connectorEditorValue(field, p.values[field.ID], task)
			if !ok {
				p.problem = tr("字段值无效") + " · " + fieldCaption(field.ID)
				return
			}
			request.Fields[field.ID] = value
		}
		if len(request.Fields) == 0 {
			m.connectorEditor011 = nil
			return
		}
	}
	if previous := m.liveTaskRequests[task.ID]; previous.RequestID != "" {
		request.RequestID = previous.RequestID
		if !reflect.DeepEqual(request, previous) {
			p.problem = tr("来源尚未确认，重试可核对本次提交")
			return
		}
	}
	p.saving = true
	if m.liveTaskRequests == nil {
		m.liveTaskRequests = map[string]common.WorkbenchTaskWrite{}
	}
	if m.liveTaskProblems == nil {
		m.liveTaskProblems = map[string]string{}
	}
	m.liveTaskRequests[task.ID] = request
	m.pendingTaskEdits[task.ID] = true
	delete(m.liveTaskProblems, task.ID)
	m.runLiveTaskWrite(request, func(receipt common.WorkbenchTaskReceipt, err error) {
		p.saving = false
		m.finishLiveTaskToggle(task.ID, receipt, err)
		if m.connectorEditor011 != p {
			return
		}
		if err != nil {
			p.problem = common.ProtocolError(err).Message
			return
		}
		m.connectorEditor011 = nil
	})
}

func (m *mockup) connectorTaskEditorDialog(c *ui.Context) {
	p := m.connectorEditor011
	if p == nil {
		return
	}
	open := true
	_, height := c.Size()
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(480).Gap(12).Children(func() {
			title := "编辑任务"
			if p.actionMode {
				title = "状态操作"
			}
			components.Title(c.Key("heading"), tr(title))
			components.Muted(c.Key("subject"), p.task.Title)
			if p.loading {
				components.Muted(c.Key("loading"), tr("正在读取来源详情"))
			}
			ui.Scroll(c.Key("form")).FillWidth().MaxHeight(max(145, min(height-245, 430))).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					if p.actionMode {
						if len(p.actions) == 0 && !p.loading {
							components.Muted(c.Key("empty"), tr("暂无可用状态操作"))
						}
						if len(p.actions) > 0 {
							m.connectorActionFields(c.Key("actions"), p)
						}
						return
					}
					if len(p.fields) == 0 && !p.loading {
						components.Muted(c.Key("empty"), tr("暂无可编辑字段"))
					}
					for _, field := range p.fields {
						if p.usesRepositoryLabels(field) {
							p.editRepositoryLabels(c.Key(field.ID), field.Options)
							continue
						}
						value := p.values[field.ID]
						if field.ID == "description" {
							ui.Field(c.Key(field.ID), fieldCaption(field.ID), func() { ui.TextArea(c.Key("body"), &value).Label(fieldCaption(field.ID)).FillWidth().Height(90) })
						} else {
							entry := components.TextField(c.Key(field.ID), fieldCaption(field.ID), &value)
							if field.ID == "due_date" {
								entry.Placeholder("YYYY-MM-DD")
							}
							if field.ID == "assignees" || field.ID == "labels" {
								entry.Placeholder(tr("用逗号分隔"))
							}
						}
						p.values[field.ID] = value
					}
				})
			})
			if p.problem != "" {
				components.Muted(c.Key("problem"), p.problem)
			}
			ui.Row(c.Key("footer")).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Disabled(p.saving).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c.Key("save"), tr("提交")).Disabled(p.loading || p.saving || p.actionMode && len(p.actions) == 0 || !p.actionMode && len(p.fields) == 0).Clicked() {
					m.submitConnectorEditor(p)
				}
			})
		})
	})
	if !open && !p.saving {
		m.connectorEditor011 = nil
	}
}

func (p *connectorTaskEditor) usesRepositoryLabels(field protocol.FieldDefinition) bool {
	return p.task.Kind == managedGitHub && field.Type == protocol.FieldLabels
}

func (p *connectorTaskEditor) editRepositoryLabels(c *ui.Context, options []protocol.Option) {
	components.Muted(c.Key("label"), fieldCaption("labels"))
	remove := -1
	for i, label := range p.repositoryLabels {
		ui.Row(c.Key("selected-" + label)).FillWidth().Gap(8).Children(func() {
			components.Body(c.Key("name"), label)
			if ui.Button(c.Key("remove"), tr("移除标签")).Clicked() {
				remove = i
			}
		})
	}
	if remove >= 0 {
		p.repositoryLabels = slices.Delete(p.repositoryLabels, remove, remove+1)
	}
	if slices.ContainsFunc(options, func(option protocol.Option) bool {
		return !slices.Contains(p.repositoryLabels, option.ID)
	}) {
		ui.MenuButton(c.Key("options"), tr("添加仓库标签"), func(menu *ui.Menu) {
			for _, option := range options {
				if !slices.Contains(p.repositoryLabels, option.ID) && menu.Item(option.Label).ID(option.ID).Chosen() {
					p.repositoryLabels = append(p.repositoryLabels, option.ID)
				}
			}
		}).FillWidth()
	}
}

func (m *mockup) connectorActionFields(c *ui.Context, p *connectorTaskEditor) {
	options := map[string]string{}
	selected := ""
	for _, action := range p.actions {
		label := connectorActionLabel(action)
		options[label] = action.ID
		if action.ID == p.action {
			selected = label
		}
	}
	ui.Select(c.Key("select"), &selected, sortedConnectorKeys(options)).FillWidth()
	if options[selected] != p.action {
		p.action = options[selected]
		p.actionValues = map[string]string{}
	}
	for _, action := range p.actions {
		if action.ID != p.action {
			continue
		}
		for _, field := range action.Required {
			value := p.actionValues[field.ID]
			components.TextField(c.Key(field.ID), connectorRequiredFieldLabel(field), &value)
			p.actionValues[field.ID] = value
		}
	}
}

func (m *mockup) connectorConflictView(c *ui.Context, task sourceObject) {
	receipt := m.connectorConflicts[task.ID]
	if receipt.Conflict == nil {
		return
	}
	_, recovering := m.connectorConflictRefreshes[task.ConnectionID]
	working := m.pendingSync[task.ConnectionID] || recovering
	if working {
		components.StatusLabel(c.Key("progress"), components.Status{Text: tr("正在同步"), Loading: true, Color: c.Theme().TextMuted})
	} else {
		components.Muted(c.Key("status"), tr("来源已变化，请重新核对任务"))
	}
	ui.Row(c.Key("actions")).FillWidth().Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("keep"), tr("接受来源值")).Disabled(working).Clicked() {
			m.resolveConnectorConflict(task, false)
		}
		if ui.Button(c.Key("edit"), tr("重新编辑")).Disabled(working).Clicked() {
			m.resolveConnectorConflict(task, true)
		}
	})
	if action := receipt.Conflict.Action; action != "" {
		components.Body(c.Key("proposed-action"), tr("状态操作")+" · "+connectorActionLabel(protocol.StateAction{ID: action, Label: action}))
		if previous := receipt.Conflict.PreviousState; previous != nil {
			components.Body(c.Key("previous-state"), tr("提交前状态")+" · "+detailValue(*previous))
		}
		for _, key := range []string{"state", "state_reason"} {
			if value, ok := receipt.Conflict.Current[key]; ok {
				components.Body(c.Key("current-"+key), fieldCaption(key)+" · "+tr("来源值")+" "+detailValue(value))
			}
		}
	}
	for _, key := range sortedConnectorKeys(receipt.Conflict.Proposed) {
		components.Body(c.Key(key), fieldCaption(key)+" · "+tr("来源值")+" "+detailValue(receipt.Conflict.Current[key])+" · "+tr("拟写值")+" "+detailValue(receipt.Conflict.Proposed[key]))
	}
}

func connectorActionLabel(action protocol.StateAction) string {
	switch action.ID {
	case "close":
		return tr("关闭 Issue")
	case "close_not_planned":
		return tr("关闭为不计划处理")
	case "reopen":
		return tr("重新打开任务")
	case "resolve":
		return tr("完成任务")
	case "done":
		return tr("完成任务")
	case "open":
		return tr("重新打开任务")
	default:
		return action.Label
	}
}

func connectorRequiredFieldLabel(field protocol.FieldDefinition) string {
	if field.ID == "resolution" {
		return tr("解决结果")
	}
	return field.Label
}

type connectorConflictRefresh struct {
	taskID           string
	attemptID        int64
	edit, actionMode bool
}

func (m *mockup) resolveConnectorConflict(task sourceObject, edit bool) {
	conflict := m.connectorConflicts[task.ID].Conflict
	actionMode := conflict != nil && conflict.Action != ""
	if !isManagedConnector(task.Kind) {
		m.openConnectorTaskEditor(task, actionMode)
		return
	}
	if _, recovering := m.connectorConflictRefreshes[task.ConnectionID]; recovering {
		return
	}
	m.trackConnectorConflictRefresh(task, 0, edit, actionMode)
	m.pendingSync[task.ConnectionID] = true
	type syncResult struct {
		AttemptID int64 `json:"attempt_id"`
	}
	connectorUIRequest(m, "workbench.sync.run", map[string]string{"id": task.ConnectionID}, func(result syncResult, err error) {
		if err != nil {
			delete(m.connectorConflictRefreshes, task.ConnectionID)
			delete(m.pendingSync, task.ConnectionID)
			m.liveTaskProblems[task.ID] = common.ProtocolError(err).Message
			return
		}
		m.trackConnectorConflictRefresh(task, result.AttemptID, edit, actionMode)
		m.refreshLive()
	})
}

func (m *mockup) trackConnectorConflictRefresh(task sourceObject, attemptID int64, edit, actionMode bool) {
	delete(m.liveTaskProblems, task.ID)
	if m.connectorConflictRefreshes == nil {
		m.connectorConflictRefreshes = make(map[string]connectorConflictRefresh)
	}
	m.connectorConflictRefreshes[task.ConnectionID] = connectorConflictRefresh{
		taskID: task.ID, attemptID: attemptID, edit: edit, actionMode: actionMode,
	}
}
