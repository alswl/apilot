package workbench

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorTaskDetailState struct {
	version           string
	connectionVersion int64
	scopeID           string
	mappingRevision   int64
	segment           string
	loading, loaded   bool
	problem           string
	page              common.WorkbenchTaskDetail
}

func (m *mockup) loadConnectorTaskSegment(taskID string, state *connectorTaskDetailState, page string) {
	if state.loading {
		return
	}
	state.loading = true
	input := map[string]string{"id": taskID, "segment": state.segment}
	if state.scopeID != "" {
		input["scope_id"] = state.scopeID
	}
	if page != "" {
		input["page"] = page
	}
	connectorUIRequest(m, "workbench.task.detail", input, func(value common.WorkbenchTaskDetail, err error) {
		detail := m.taskDetails[taskID]
		if detail == nil || detail.connectorDetail != state && detail.connectorHeader != state {
			return
		}
		state.loading, state.loaded = false, true
		state.problem = ""
		if err != nil {
			state.problem = common.ProtocolError(err).Message
			return
		}
		if page != "" && state.page.Version == value.Version {
			value.Items = append(state.page.Items, value.Items...)
		}
		state.page = value
	})
}

func detailValue(field protocol.FieldValue) string {
	if len(field.Value) == 0 || string(field.Value) == "null" {
		return "—"
	}
	var text string
	if json.Unmarshal(field.Value, &text) == nil {
		return text
	}
	if field.Type == protocol.FieldOption || field.Type == protocol.FieldAccount {
		var option protocol.Option
		if json.Unmarshal(field.Value, &option) == nil && option.ID != "" {
			return optionCaption(option)
		}
	}
	if field.Type == protocol.FieldAccounts || field.Type == protocol.FieldOptions {
		var options []protocol.Option
		if json.Unmarshal(field.Value, &options) == nil {
			values := make([]string, 0, len(options))
			for _, option := range options {
				values = append(values, optionCaption(option))
			}
			return strings.Join(values, ", ")
		}
	}
	return string(field.Value)
}

func optionCaption(option protocol.Option) string {
	if option.Label != "" {
		return option.Label
	}
	return option.ID
}

func (m *mockup) connectorTaskHeader(c *ui.Context, task *sourceObject, showScope bool) {
	if m.live == nil || !m.isManagedConnector(task.Kind) {
		return
	}
	detail := m.taskDetailUI(task.ID)
	state := detail.connectorHeader
	revision := m.connectorMappingRevision(task.ConnectionID)
	if state == nil || state.version != task.TargetVersion || state.scopeID != task.ConnectionID ||
		state.connectionVersion != task.ConnectionVersion || state.mappingRevision != revision {
		state = &connectorTaskDetailState{version: task.TargetVersion, scopeID: task.ConnectionID,
			connectionVersion: task.ConnectionVersion, mappingRevision: revision, segment: "top_fields"}
		detail.connectorHeader = state
	}
	if !state.loading && !state.loaded {
		m.loadConnectorTaskSegment(task.ID, state, "")
	}
	if state.loading {
		components.StatusLabel(c.Key("loading"), components.Status{Text: tr("正在读取关键来源字段"), Loading: true, Color: c.Theme().TextMuted})
	}
	if len(state.page.TopFields) > 0 {
		fields := make([]components.Property, 0, len(state.page.TopFields)+1)
		if showScope {
			label := m.connectionName(task.ConnectionID)
			if scope := m.connectorTaskScope(task.ConnectionID); scope != nil {
				label = scope.Label
				if kind := scopeTypeLabel(scope.ScopeType); kind != "" {
					label += " · " + kind
				}
			}
			fields = append(fields, components.Property{ID: "connection-scope", Title: tr("同步范围"), Value: label})
		}
		for _, field := range state.page.TopFields {
			fields = append(fields, components.Property{ID: "top/" + field.ID, Title: field.Title, Value: detailValue(field.Value)})
		}
		components.PropertyGrid(c.Key("top-fields"), fields, 2)
	}
	if state.page.Cached {
		components.Muted(c.Key("cached"), tr("已缓存")+" · "+state.page.CapturedAt.Local().Format(time.DateTime))
	}
	if state.problem != "" {
		components.Muted(c.Key("error"), tr("无法读取关键来源字段")+" · "+state.problem)
		if ui.Button(c.Key("retry"), tr("重试")).Clicked() {
			m.loadConnectorTaskSegment(task.ID, state, "")
		}
	}
}

func (m *mockup) connectorTaskScope(scopeID string) *common.WorkbenchConnectorScopeOverview {
	for _, parent := range m.connectorParents {
		for i := range parent.Scopes {
			if parent.Scopes[i].ScopeID == scopeID {
				return &parent.Scopes[i]
			}
		}
	}
	return nil
}

func (m *mockup) connectorMappingRevision(scopeID string) int64 {
	if scope := m.connectorTaskScope(scopeID); scope != nil {
		return scope.MappingRevision
	}
	return 0
}

func (m *mockup) managedTaskDetail(c *ui.Context, task *sourceObject, detail *taskDetailUI) {
	state := detail.connectorDetail
	mappingRevision := m.connectorMappingRevision(task.ConnectionID)
	if state == nil || state.version != task.TargetVersion || state.mappingRevision != mappingRevision || state.connectionVersion != task.ConnectionVersion {
		segment := "description"
		if state != nil {
			segment = state.segment
		}
		state = &connectorTaskDetailState{version: task.TargetVersion, scopeID: task.ConnectionID, mappingRevision: mappingRevision, connectionVersion: task.ConnectionVersion, segment: segment}
		detail.connectorDetail = state
	}
	segments := map[string]string{"来源字段": "fields", "描述": "description"}
	if task.Kind != managedGitHub {
		segments["来源评论"] = "comments"
		segments["附件"] = "attachments"
		segments["链接"] = "links"
		segments["历史"] = "history"
	}
	selected := "描述"
	for label, id := range segments {
		if state.segment == id {
			selected = label
			break
		}
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c.Key("actions")).FillWidth().Wrap().Gap(8).Children(func() {
			if usageChoice(c.Key("section"), &selected, segments, "描述", 130) && state.segment != segments[selected] {
				state = &connectorTaskDetailState{version: task.TargetVersion, scopeID: task.ConnectionID, mappingRevision: mappingRevision, connectionVersion: task.ConnectionVersion, segment: segments[selected]}
				detail.connectorDetail = state
			}
			openURLButton(c.Key("open"), tr("打开原任务"), task.Locator)
			if ui.Button(c.Key("mapping"), tr("字段映射")).Clicked() {
				m.openConnectorMapping(task.ConnectionID, task.ID)
			}
		})
		if !state.loaded && !state.loading {
			m.loadConnectorTaskSegment(task.ID, state, "")
		}
		if state.loading {
			components.Muted(c.Key("loading"), tr("正在读取来源详情"))
		}
		if state.problem != "" {
			components.Muted(c.Key("error"), state.problem)
			if ui.Button(c.Key("retry"), tr("重试")).Clicked() {
				m.loadConnectorTaskSegment(task.ID, state, "")
			}
		}
		if state.page.Cached {
			components.Muted(c.Key("cached"), tr("已缓存")+" · "+state.page.CapturedAt.Local().Format(time.DateTime))
		}
		for _, id := range sortedConnectorKeys(state.page.Fields) {
			components.PropertyGrid(c.Key(id), []components.Property{{ID: id, Title: fieldCaption(id), Value: detailValue(state.page.Fields[id])}}, 1)
		}
		for _, item := range state.page.Items {
			ui.Column(c.Key(item.ID)).FillWidth().Gap(4).Children(func() {
				if item.URL != "" {
					openURLButton(c.Key("link"), item.Label, item.URL)
				} else if state.segment != "description" {
					components.Muted(c.Key("label"), item.Label)
				}
				if item.Text != nil {
					components.Body(c.Key("text"), detailValue(protocol.FieldValue{Value: item.Text.Content}))
				}
			})
		}
		if state.loaded && !state.loading && state.problem == "" && len(state.page.Items) == 0 && len(state.page.Fields) == 0 {
			components.Muted(c.Key("empty"), tr("暂无来源详情"))
		}
		if state.page.NextPage != "" && ui.Button(c.Key("more"), tr("加载更多")).Disabled(state.loading).Clicked() {
			m.loadConnectorTaskSegment(task.ID, state, state.page.NextPage)
		}
	})
}
