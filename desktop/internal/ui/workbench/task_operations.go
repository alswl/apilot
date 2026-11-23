package workbench

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	obsidianTasks = common.WorkbenchObsidianTasks
	githubIssues  = common.WorkbenchGitHubIssues
)

type taskSourceEdit struct {
	id, kind, title, body, state, assignees, milestone string
	labelQuery                                         string
	labels                                             []string
	due, scheduled, start, priority, problem           string
}

func (m *mockup) taskConnection(o sourceObject) *connection {
	for i := range m.connectionsData {
		if m.connectionsData[i].ID == o.ConnectionID {
			return &m.connectionsData[i]
		}
	}
	return nil
}

func (m *mockup) editableSource(o sourceObject) bool {
	if m.live != nil {
		return m.supportsConnectorEditor(o)
	}
	conn := m.taskConnection(o)
	return m.live == nil && conn != nil && conn.Writable && conn.Status == "同步正常" &&
		(conn.SourceKind == obsidianTasks || conn.SourceKind == githubIssues)
}

func sourceDraft(o sourceObject, kind string) taskSourceEdit {
	draft := taskSourceEdit{
		id: o.ID, kind: kind, title: tr(o.Title), body: tr(o.Summary),
		state: sourceState(o, kind), labels: slices.Clone(o.Labels), assignees: strings.Join(o.Assignees, ", "),
		priority: o.NativeFields["priority"], milestone: o.NativeFields["milestone"],
		due: dateForEditor(o.NativeFields["due"]), scheduled: dateForEditor(o.NativeFields["scheduled"]),
		start: dateForEditor(o.NativeFields["start"]),
	}
	if draft.priority == "" {
		draft.priority = "普通"
	}

	return draft
}

func parseTaskDate(value string) time.Time {
	if date, err := time.Parse(time.DateOnly, value); err == nil {
		return date
	}
	if date, err := time.Parse("1 月 2 日", value); err == nil {
		return date.AddDate(2026, 0, 0)
	}
	return time.Time{}
}

func dateForEditor(value string) string {
	if date := parseTaskDate(value); !date.IsZero() {
		return date.Format(time.DateOnly)
	}
	return value
}

func (m *mockup) sourceWritePending(id string) bool {
	return m.pendingTaskEdits[id] || workbenchOperationInProgress(m.serverTaskOperations[id].SyncState) || (id == m.mutation.SourceObject && m.mutation.Status == "queued")
}

func (m *mockup) openSourceEdit(o sourceObject) {
	if m.live != nil {
		m.openConnectorTaskEditor(o, false)
		return
	}
	if !m.editableSource(o) || m.sourceWritePending(o.ID) {
		return
	}
	draft := sourceDraft(o, m.taskConnection(o).SourceKind)
	m.sourceEditor = &draft
}

func (m *mockup) taskSourceActions(c *ui.Context, o *sourceObject) {
	if m.live != nil {
		if m.supportsConnectorEditor(*o) {
			ui.Row(c.Key("edit-toolbar")).FillWidth().Wrap().Gap(8).Children(func() {
				blocked := m.sourceWritePending(o.ID) || m.serverTaskOperations[o.ID].SyncState == common.WorkbenchOperationNeedsReview
				if components.IconButton(c.Key("edit"), "pencil", tr("编辑任务"), blocked).Clicked() {
					m.openConnectorTaskEditor(*o, false)
				}
				if isManagedConnector(o.Kind) && ui.Button(c.Key("transition"), tr("状态操作")).Disabled(blocked).Clicked() {
					m.openConnectorTaskEditor(*o, true)
				}
			})
		}
		if !isManagedConnector(o.Kind) && (m.canToggleSourceTask(*o) || m.sourceWritePending(o.ID)) {
			m.taskStatusCheckbox(c.Key("toggle"), *o)
		}
		if m.sourceWritePending(o.ID) {
			components.Muted(c.Key("pending"), tr("等待来源确认"))
		}
		if problem := m.liveTaskProblems[o.ID]; problem != "" {
			components.Muted(c.Key("write-problem"), tr(problem))
		}
		m.connectorConflictView(c.Key("conflict"), *o)
		if receipt := m.serverTaskOperations[o.ID]; receipt.SyncState == common.WorkbenchOperationNeedsReview && !m.sourceWritePending(o.ID) {
			components.Muted(c.Key("review-status"), tr("来源尚未确认"))
			if ui.Button(c.Key("reconcile"), tr("核对操作")).Clicked() {
				m.reconcileLiveTask(o.ID, receipt.RequestID)
			}
		}
		return
	}
	conn := m.taskConnection(*o)
	if conn == nil || !conn.Writable {
		return
	}
	if conn.Status != "同步正常" {
		components.Muted(c, tr("连接断开，恢复后可编辑"))
		if ui.Button(c.Key("reconnect"), tr("重新连接")).Disabled(m.pendingSync[conn.ID]).Clicked() {
			m.syncConnection(conn, m.after)
		}
		return
	}
	if m.sourceWritePending(o.ID) {
		components.StatusLabel(c.Key("pending"), components.Status{Text: tr("等待来源确认"), Loading: true, Color: c.Theme().Warning})
		return
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		edit := components.IconButton(c.Key("edit"), "pencil", tr("编辑任务"), false)
		if shortcut := m.keyboard.display("task.edit"); shortcut != "" {
			edit.Tooltip(tr("编辑任务") + " · " + shortcut)
		}
		if edit.Clicked() {
			m.openSourceEdit(*o)
		}
		m.taskStatusCheckbox(c.Key("toggle"), *o)
	})
}

func (m *mockup) sourceToggleLabel(o sourceObject) string {
	if conn := m.taskConnection(o); conn != nil && (conn.SourceKind == githubIssues || conn.SourceKind == common.WorkbenchGitHubRepository || conn.SourceKind == managedGitHub) {
		if o.NativeFields["state"] == "closed" {
			return "重新打开 Issue"
		}
		return "关闭 Issue"
	}
	if taskClosed(o) {
		return "重新打开任务"
	}
	return "完成任务"
}

func (m *mockup) canToggleSourceTask(o sourceObject) bool {
	if m.live != nil && isManagedConnector(o.Kind) {
		return m.supportsConnectorEditor(o) && o.TargetVersion != ""
	}
	if m.live != nil {
		if o.Kind == common.WorkbenchObsidianTasks && o.State == "unknown" {
			return false
		}
		conn := m.taskConnection(o)
		return conn != nil && conn.Writable && (o.Kind == common.WorkbenchLocal || (o.Kind == common.WorkbenchObsidianTasks || o.Kind == common.WorkbenchGitHubRepository) && conn.Status == "同步正常") && o.TargetVersion != ""
	}
	return m.editableSource(o)
}

func (m *mockup) toggleSourceTask(o sourceObject) {
	if m.live != nil && isManagedConnector(o.Kind) {
		m.openConnectorTaskEditor(o, true)
		return
	}
	if m.live != nil {
		if !m.canToggleSourceTask(o) || m.sourceWritePending(o.ID) || m.serverTaskOperations[o.ID].SyncState == common.WorkbenchOperationNeedsReview {
			return
		}
		state := "done"
		if taskClosed(o) {
			state = "open"
		}
		m.submitLiveTaskState(o, state)
		return
	}
	state := "已完成"
	if taskClosed(o) {
		if conn := m.taskConnection(o); conn != nil {
			state = conn.reopenState()
		}
	}
	m.setTaskState(o, state)
}

func (m *mockup) submitLiveTaskState(o sourceObject, state string) {
	if m.live != nil && m.canToggleSourceTask(o) && !m.sourceWritePending(o.ID) && m.serverTaskOperations[o.ID].SyncState != common.WorkbenchOperationNeedsReview {
		if m.liveTaskRequests == nil {
			m.liveTaskRequests = make(map[string]common.WorkbenchTaskWrite)
			m.liveTaskProblems = make(map[string]string)
		}
		request := m.liveTaskRequests[o.ID]
		if request.RequestID == "" {
			request = common.WorkbenchTaskWrite{RequestID: common.ID(), SourceObjectID: o.ID, ConnectionVersion: o.ConnectionVersion, TargetVersion: o.TargetVersion, State: state}
			m.liveTaskRequests[o.ID] = request
		}
		m.pendingTaskEdits[o.ID] = true
		delete(m.liveTaskProblems, o.ID)
		m.runLiveTaskWrite(request, func(receipt common.WorkbenchTaskReceipt, err error) {
			m.finishLiveTaskToggle(o.ID, receipt, err)
		})
	}
}

func (m *mockup) reconcileLiveTask(taskID, requestID string) {
	if m.live == nil || requestID == "" || m.sourceWritePending(taskID) {
		return
	}
	if m.liveTaskProblems == nil {
		m.liveTaskProblems = make(map[string]string)
	}
	m.pendingTaskEdits[taskID] = true
	remote := m.live
	go func() {
		receipt, err := remote.ReconcileWorkbenchTaskOperation(requestID)
		mygo.RunOnMain(func() {
			if m.pendingTaskEdits[taskID] {
				m.finishLiveTaskToggle(taskID, receipt, err)
			}
			m.liveGeneration++
			if m.win != nil && !m.win.IsDestroyed() {
				m.win.Invalidate()
				m.refreshLive()
			}
		})
	}()
}

func (m *mockup) finishLiveTaskToggle(id string, receipt common.WorkbenchTaskReceipt, err error) {
	if err == nil && workbenchOperationInProgress(receipt.SyncState) {
		return
	}
	delete(m.pendingTaskEdits, id)
	switch {
	case err != nil:
		problem := common.ProtocolError(err)
		m.liveTaskProblems[id] = problem.Message
		if problem.Code == "STALE_REVISION" || problem.Code == "INVALID_ARGUMENT" || problem.Code == "CONFLICT" {
			delete(m.liveTaskRequests, id)
		}
	case receipt.SyncState == "confirmed":
		delete(m.connectorConflicts, id)
		delete(m.liveTaskRequests, id)
		delete(m.liveTaskProblems, id)
	case receipt.SyncState == "conflict":
		if receipt.Conflict != nil {
			if m.connectorConflicts == nil {
				m.connectorConflicts = map[string]common.WorkbenchTaskReceipt{}
			}
			m.connectorConflicts[id] = receipt
		}
		delete(m.liveTaskRequests, id)
		m.liveTaskProblems[id] = receipt.Reason
		if receipt.Reason == "" {
			m.liveTaskProblems[id] = "来源已变化，请重新核对任务"
		}
	case receipt.SyncState == common.WorkbenchOperationNeedsReview:
		if m.serverTaskOperations == nil {
			m.serverTaskOperations = map[string]common.WorkbenchTaskReceipt{}
		}
		m.serverTaskOperations[id] = receipt
		m.liveTaskProblems[id] = "来源尚未确认，重试可核对本次提交"
	default:
		delete(m.liveTaskRequests, id)
		m.liveTaskProblems[id] = receipt.Reason
	}
}

func (m *mockup) setTaskState(o sourceObject, state string) {
	if m.live != nil {
		if state == "待处理" || state == "开放" {
			if o.State != "open" {
				m.submitLiveTaskState(o, "open")
			}
		} else if state == "已完成" && o.State != "done" {
			m.submitLiveTaskState(o, "done")
		}
		return
	}
	if !m.editableSource(o) || m.sourceWritePending(o.ID) {
		return
	}
	draft := sourceDraft(o, m.taskConnection(o).SourceKind)
	draft.state = state
	m.submitSourceEdit(draft, m.after)
}

func (m *mockup) sourceEditDialog(c *ui.Context) {
	open := m.sourceEditor != nil
	_, height := c.Size()
	ui.Modal(c, &open, func() {
		edit := m.sourceEditor
		ui.Column(c).Width(500).Gap(12).Children(func() {
			components.Title(c, tr("编辑任务"))
			ui.Scroll(c).FillWidth().MaxHeight(max(200, min(520, height-145))).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					components.TextField(c.Key("title"), tr("任务名称"), &edit.title).AutoFocus()
					if edit.kind == githubIssues {
						ui.Field(c.Key("body"), tr("描述"), func() {
							ui.TextArea(c, &edit.body).Label(tr("描述")).FillWidth().Height(72)
						})
						choiceField(c.Key("state"), "状态", &edit.state, []string{"开放", "已完成", "不计划处理"})
						m.sourceLabelField(c.Key("labels"), edit, "仓库标签")
						components.TextField(c.Key("assignees"), tr("负责人"), &edit.assignees).Placeholder(tr("用逗号分隔"))
						for _, o := range m.objects {
							if o.ID == edit.id {
								conn := m.taskConnection(o)
								milestone := edit.milestone
								if milestone == "" {
									milestone = "无里程碑"
								}
								choiceField(c.Key("milestone"), "里程碑", &milestone, append([]string{"无里程碑"}, conn.IssueMilestones...))
								edit.milestone = milestone
								if milestone == "无里程碑" {
									edit.milestone = ""
								}
								components.Muted(c, tr("可用负责人：")+strings.Join(conn.IssueAssignees, ", "))
								break
							}
						}
					} else {
						choiceField(c.Key("state"), "状态", &edit.state, []string{"待处理", "进行中", "已完成", "不计划处理"})
						choiceField(c.Key("priority"), "优先级", &edit.priority, []string{"最高", "高", "中", "普通", "低", "最低"})
						m.sourceLabelField(c.Key("labels"), edit, "Obsidian 标签")
						components.TextField(c.Key("due"), tr("截止日"), &edit.due).Placeholder("YYYY-MM-DD")
						components.TextField(c.Key("scheduled"), tr("计划日期"), &edit.scheduled).Placeholder("YYYY-MM-DD")
						components.TextField(c.Key("start"), tr("开始日期"), &edit.start).Placeholder("YYYY-MM-DD")
					}
				})
			})
			if edit.problem != "" {
				components.Muted(c, edit.problem)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c.Key("save"), tr("提交")).Clicked() {
					edit.problem = m.submitSourceEdit(*edit, m.after)
					open = edit.problem != ""
				}
			})
		})
	})
	if !open {
		m.sourceEditor = nil
	}
}

func (m *mockup) sourceLabelField(c *ui.Context, edit *taskSourceEdit, label string) {
	for _, o := range m.objects {
		if o.ID == edit.id {
			if conn := m.taskConnection(o); conn != nil {
				m.labelPicker(c, conn, label, &edit.labelQuery, &edit.labels, false)
			}
			return
		}
	}
}

func choiceField(c *ui.Context, label string, value *string, options []string) {
	ui.Field(c, tr(label), func() { localizedSelect(c, value, options, 0) })
}

func splitValues(text string) []string {
	var result []string
	for _, value := range strings.Split(text, ",") {
		value = strings.TrimSpace(value)
		if value != "" && !slices.Contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func (m *mockup) submitSourceEdit(edit taskSourceEdit, after func(time.Duration, func())) string {
	index := slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == edit.id })
	if index < 0 || !m.editableSource(m.objects[index]) {
		return tr("来源当前无法写入")
	}
	if m.sourceWritePending(edit.id) {
		return tr("等待来源确认")
	}
	title := strings.TrimSpace(edit.title)
	if title == "" {
		return tr("请输入任务名称")
	}
	o := m.objects[index]
	confirmed := sourceDraft(o, edit.kind)
	labels := slices.Clone(edit.labels)
	assignees := splitValues(edit.assignees)
	originalFields := maps.Clone(o.NativeFields)
	fields := maps.Clone(originalFields)
	body := o.Summary
	if title == confirmed.title {
		title = o.Title
	}
	if edit.kind != m.taskConnection(o).SourceKind {
		return tr("来源当前无法写入")
	}
	if edit.kind == githubIssues {
		conn := m.taskConnection(o)
		for _, label := range labels {
			if !slices.Contains(conn.IssueLabels, label) {
				return tr("该仓库没有此标签：") + label
			}
		}
		for _, person := range assignees {
			if !slices.Contains(conn.IssueAssignees, person) {
				return tr("该仓库无法指派给：") + person
			}
		}
		if edit.milestone != "" && !slices.Contains(conn.IssueMilestones, edit.milestone) {
			return tr("里程碑不可用")
		}
		if edit.body != confirmed.body {
			body = edit.body
		}
		if edit.milestone != confirmed.milestone {
			if edit.milestone == "" {
				delete(fields, "milestone")
			} else {
				fields["milestone"] = edit.milestone
			}
		}
		if edit.state != confirmed.state {
			fields["state"], fields["status"] = "open", "待处理"
			delete(fields, "state_reason")
			delete(fields, "closed_at")
			if edit.state != "开放" {
				if o.NativeFields["closed_at"] == "" {
					fields["closed_at"] = time.Now().Format(time.RFC3339)
				} else {
					fields["closed_at"] = o.NativeFields["closed_at"]
				}
				fields["state"], fields["state_reason"], fields["status"] = "closed", "completed", "已完成"
				if edit.state == "不计划处理" {
					fields["state_reason"], fields["status"] = "not_planned", "不计划处理"
				}
			}
		}
	} else {
		for _, label := range labels {
			if !validObsidianLabel(label) {
				return tr("无效的 Obsidian 标签：") + label
			}
		}
		for field, value := range map[string]string{"due": edit.due, "scheduled": edit.scheduled, "start": edit.start} {
			value = strings.TrimSpace(value)
			if value != "" {
				if _, err := time.Parse(time.DateOnly, value); err != nil {
					return tr("日期格式应为 YYYY-MM-DD")
				}
			}
			if value != dateForEditor(fields[field]) {
				fields[field] = value
			}
		}
		fields["priority"] = edit.priority
		if edit.state != confirmed.state {
			fields["status"] = edit.state
			options := m.taskConnection(o).stateMappings()
			index := slices.IndexFunc(options, func(option taskStateMapping) bool { return option.label == edit.state })
			if index < 0 {
				return tr("来源当前无法写入")
			}
			fields["checkbox"] = options[index].native
			if edit.state == "已完成" {
				fields["done"] = time.Now().Format(time.DateOnly)
			}
			if edit.state != "已完成" {
				delete(fields, "done")
			}
		}
	}
	if title == o.Title && body == o.Summary && maps.Equal(fields, o.NativeFields) &&
		sameLabelSet(labels, o.Labels) && slices.Equal(assignees, o.Assignees) {
		return ""
	}
	m.pendingTaskEdits[edit.id] = true
	after(650*time.Millisecond, func() {
		// Locate the object by stable ID even when the user switches selection.
		i := slices.IndexFunc(m.objects, func(item sourceObject) bool { return item.ID == edit.id })
		delete(m.pendingTaskEdits, edit.id)
		if i < 0 {
			return
		}
		task := &m.objects[i]
		if title != o.Title {
			task.Title = title
		}
		if body != o.Summary {
			task.Summary = body
		}
		if !sameLabelSet(labels, o.Labels) {
			task.Labels = labels
		}
		if !slices.Equal(assignees, o.Assignees) {
			task.Assignees = assignees
		}
		services.ApplyWorkbenchTaskFields(task, originalFields, fields)
		m.recordTaskEvent(edit.id, "来源确认了任务修改")
	})
	return ""
}

func taskClosed(o sourceObject) bool {
	if (o.Kind == common.WorkbenchGitHubRepository || o.Kind == managedGitHub) && o.NativeFields["state"] == "closed" {
		return true
	}
	if o.State != "" {
		return o.State == "done" || o.State == "cancelled"
	}
	return o.NativeFields["state"] == "closed" || o.NativeFields["status"] == "已完成" || o.NativeFields["status"] == "不计划处理"
}

func displayTaskID(o sourceObject) string {
	if o.NativeFields["repo"] != "" || o.Kind == common.WorkbenchGitHubRepository || o.Kind == managedGitHub {
		return "#" + o.NativeID
	}
	return o.NativeID
}
