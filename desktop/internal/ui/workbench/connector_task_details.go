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
	version         string
	mappingRevision int64
	segment         string
	loading, loaded bool
	problem         string
	page            common.WorkbenchTaskDetail
}

func (m *mockup) loadConnectorTaskSegment(taskID string, state *connectorTaskDetailState, page string) {
	if state.loading {
		return
	}
	state.loading = true
	input := map[string]string{"id": taskID, "segment": state.segment}
	if page != "" {
		input["page"] = page
	}
	connectorUIRequest(m, "workbench.task.detail", input, func(value common.WorkbenchTaskDetail, err error) {
		detail := m.taskDetails[taskID]
		if detail == nil || detail.connectorDetail != state {
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
	if field.Type == protocol.FieldAccounts {
		var options []protocol.Option
		if json.Unmarshal(field.Value, &options) == nil {
			values := make([]string, 0, len(options))
			for _, option := range options {
				values = append(values, option.Label)
			}
			return strings.Join(values, ", ")
		}
	}
	return string(field.Value)
}

func (m *mockup) connectorMappingRevision(scopeID string) int64 {
	for _, parent := range m.connectorParents {
		for _, scope := range parent.Scopes {
			if scope.ScopeID == scopeID {
				return scope.MappingRevision
			}
		}
	}
	return 0
}

func (m *mockup) managedTaskDetail(c *ui.Context, task *sourceObject, detail *taskDetailUI) {
	state := detail.connectorDetail
	mappingRevision := m.connectorMappingRevision(task.ConnectionID)
	if state == nil || state.version != task.TargetVersion || state.mappingRevision != mappingRevision {
		segment := "description"
		if state != nil {
			segment = state.segment
		}
		state = &connectorTaskDetailState{version: task.TargetVersion, mappingRevision: mappingRevision, segment: segment}
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
				state = &connectorTaskDetailState{version: task.TargetVersion, mappingRevision: mappingRevision, segment: segments[selected]}
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
