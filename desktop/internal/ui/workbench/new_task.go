package workbench

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) writableConnections() []string {
	var sources []string
	for _, conn := range m.connectionsData {
		if conn.Writable && (conn.Status == "同步正常" || conn.Status == "可用") {
			sources = append(sources, m.newTaskConnectionLabel(conn))
		}
	}
	return sources
}

func (m *mockup) newTaskConnectionLabel(conn connection) string {
	for _, other := range m.connectionsData {
		if other.ID != conn.ID && other.Name == conn.Name {
			if conn.Repository != "" {
				unique := true
				for _, candidate := range m.connectionsData {
					if candidate.ID != conn.ID && candidate.Name == conn.Name && candidate.Repository == conn.Repository {
						unique = false
					}
				}
				if unique {
					return conn.Name + " · " + conn.Repository
				}
			}
			return conn.Name + " · " + conn.ID
		}
	}
	return conn.Name
}

func (m *mockup) bindNewTaskSource() {
	m.newTaskSourceID = ""
	for _, conn := range m.connectionsData {
		if m.newTaskConnectionLabel(conn) == m.newTaskSource {
			m.newTaskSourceID = conn.ID
			return
		}
	}
}

func (m *mockup) openNewTask() {
	if m.newTaskPending {
		m.newTaskOpen = true
		return
	}
	if m.newTaskRequest.RequestID != "" && m.newTaskTargets.problem != "" {
		m.newTaskOpen = true
		return
	}
	sources := m.writableConnections()
	if len(sources) == 0 {
		return
	}
	m.newTaskOpen = true
	m.newTaskRequest = common.WorkbenchTaskWrite{}
	m.newTaskTargets = taskDestinations{}
	m.newTaskDraft, m.newTaskBody = "", ""
	m.newTaskSource = sources[0]
	if m.sourceFilter != "" && m.sourceFilter != "全部来源" {
		selected := m.sourceFilter
		if m.sourceFilterID != "" {
			if conn := m.connectionByID(m.sourceFilterID); conn != nil && conn.Name == m.sourceFilter {
				selected = m.newTaskConnectionLabel(*conn)
			}
		}
		if slices.Contains(sources, selected) {
			m.newTaskSource = selected
		} else {
			m.newTaskSource = "选择来源"
		}
	}
	m.bindNewTaskSource()
}

func (m *mockup) newTaskDialog(c *ui.Context) {
	open := m.newTaskOpen
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(440).Gap(12).Children(func() {
			components.Title(c, tr("新建任务"))
			if m.newTaskPending {
				if m.devTask != nil {
					components.Muted(c, m.newTaskDraft)
					if ui.Button(c.Key("dev-unconfirmed"), "Dev: unconfirmed").Clicked() {
						m.resolveDevTask("needs_review")
						open = m.newTaskOpen
					}
					if ui.Button(c.Key("dev-confirm"), "Dev: confirm").Clicked() {
						m.resolveDevTask("confirmed")
						open = m.newTaskOpen
					}
				}
				components.StatusLabel(c.Key("pending"), components.Status{Text: tr("等待来源确认"), Loading: true, Color: c.Theme().Warning})
				if ui.Button(c.Key("background"), tr("后台处理")).Clicked() {
					open = false
				}
				return
			}
			var submitted bool
			ui.Field(c.Key("title-field"), tr("任务名称"), func() {
				submitted = ui.TextInput(c.Key("title"), &m.newTaskDraft).Label(tr("任务名称")).
					AutoFocus().FillWidth().Submitted()
			})
			sources := m.writableConnections()
			if m.newTaskSourceID != "" {
				conn := m.connectionByID(m.newTaskSourceID)
				if conn == nil || m.newTaskConnectionLabel(*conn) != m.newTaskSource || !slices.Contains(sources, m.newTaskSource) {
					m.newTaskSource, m.newTaskSourceID = "选择来源", ""
				}
			}
			if m.newTaskSource == "选择来源" {
				sources = append([]string{"选择来源"}, sources...)
			}
			ui.Field(c.Key("source-field"), tr("来源"), func() {
				previous := m.newTaskSource
				localizedSelect(c, &m.newTaskSource, sources, 0)
				if previous != m.newTaskSource {
					m.bindNewTaskSource()
				}
			})
			validTarget := m.newTaskSourceID != "" && m.newTaskSource != "选择来源" && slices.Contains(m.writableConnections(), m.newTaskSource)
			if m.live != nil {
				validTarget = m.newTaskDestinationFields(c.Key("destination")) && validTarget
			}
			if m.newTaskTargets.problem != "" {
				components.Muted(c, tr(m.newTaskTargets.problem))
			}
			title := strings.TrimSpace(m.newTaskDraft)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					open = false
					return
				}
				save := ui.PrimaryButton(c.Key("create"), tr("创建任务")).Disabled(title == "" || len(sources) == 0 || !validTarget)
				if (save.Clicked() || submitted) && title != "" && validTarget {
					m.createTask(title)
					open = m.newTaskOpen
					c.Invalidate()
				}
			})
		})
	})
	m.newTaskOpen = open
}

func (m *mockup) createTask(title string) {
	if m.newTaskPending {
		return
	}
	for _, conn := range m.connectionsData {
		if conn.ID != m.newTaskSourceID || m.newTaskConnectionLabel(conn) != m.newTaskSource || !conn.Writable || (conn.Status != "同步正常" && conn.Status != "可用") {
			continue
		}
		if m.live != nil || m.devTask != nil {
			request := common.WorkbenchTaskWrite{Title: title, ConnectionID: conn.ID, ConnectionVersion: conn.Version}
			if conn.SourceKind == common.WorkbenchGitHubRepository || conn.SourceKind == managedGitHub {
				request.Body = m.newTaskBody
			}
			if conn.SourceKind == obsidianTasks {
				destinations := m.newTaskTargets
				heading, ok := destinations.byLabel[destinations.heading]
				if !ok || destinations.connectionID != conn.ID || heading.TargetVersion == "" {
					return
				}
				request.NotePath, request.HeadingPath, request.TargetVersion = destinations.note, heading.HeadingPath, heading.TargetVersion
			}
			prior := m.newTaskRequest
			if prior.RequestID != "" && prior.Title == request.Title && prior.Body == request.Body && prior.ConnectionID == request.ConnectionID &&
				prior.NotePath == request.NotePath && slices.Equal(prior.HeadingPath, request.HeadingPath) {
				request = prior
			} else {
				request.RequestID = common.ID()
				m.newTaskRequest = request
			}
			m.newTaskPending = true
			m.newTaskTargets.problem = ""
			if m.devTask != nil {
				m.devTask.requests = append(m.devTask.requests, request)
			} else {
				m.runLiveTaskWrite(request, m.finishLiveTaskCreate)
			}
			return
		}
		id := fmt.Sprintf("MOCK-%d", len(m.objects)+1)
		task := sourceObject{
			ID: id, NativeID: id, ConnectionID: conn.ID, Kind: "task",
			Title: title, Locator: conn.Name + " › " + id,
			NativeFields: map[string]string{"status": "待处理", "priority": "普通", "checkbox": " "},
		}
		if conn.SourceKind == githubIssues {
			number := 0
			for _, existing := range m.objects {
				if existing.NativeFields["repo"] == conn.Repository {
					issue, _ := strconv.Atoi(existing.NativeID)
					number = max(number, issue)
				}
			}
			task.NativeID = strconv.Itoa(number + 1)
			task.Locator = conn.Repository + "#" + task.NativeID
			task.NativeFields = map[string]string{"status": "待处理", "state": "open", "repo": conn.Repository}
		}
		m.objects = append(m.objects, task)
		m.newTaskOpen = false
		m.navigate(tasksPage)
		m.selectTaskView("")
		m.search, m.statusFilter, m.sourceFilter = "", "全部状态", "全部来源"
		m.sourceFilterID = ""
		m.restoreTaskFocus = false
		m.recordTaskEvent(id, "在模拟来源创建了任务")
		m.openFullTask(len(m.objects) - 1)
		return
	}
}

func (m *mockup) finishLiveTaskCreate(receipt common.WorkbenchTaskReceipt, err error) {
	if err == nil && workbenchOperationInProgress(receipt.SyncState) {
		return
	}
	m.newTaskPending = false
	if err != nil {
		problem := common.ProtocolError(err)
		m.newTaskTargets.problem = problem.Message
		if problem.Code == "STALE_REVISION" {
			m.newTaskRequest = common.WorkbenchTaskWrite{}
		}
		return
	}
	switch receipt.SyncState {
	case "confirmed":
		wasOpen := m.newTaskOpen
		m.newTaskOpen = false
		m.newTaskRequest = common.WorkbenchTaskWrite{}
		if wasOpen {
			m.navigate(tasksPage)
		}
	case "conflict":
		m.newTaskTargets.problem = "来源已变化，请刷新写入位置"
		m.newTaskRequest = common.WorkbenchTaskWrite{}
	default:
		m.newTaskTargets.problem = "来源尚未确认，重试可核对本次提交"
	}
}

type taskDestinations struct {
	connectionID  string
	version       int64
	notes         []string
	note, heading string
	labels        []string
	byLabel       map[string]common.WorkbenchHeading
	problem       string
}

func (m *mockup) newTaskDestinationFields(c *ui.Context) bool {
	var connection *connection
	for i := range m.connectionsData {
		if m.connectionsData[i].ID == m.newTaskSourceID && m.newTaskConnectionLabel(m.connectionsData[i]) == m.newTaskSource {
			connection = &m.connectionsData[i]
			break
		}
	}
	if connection == nil {
		return false
	}
	d := &m.newTaskTargets
	if d.connectionID != connection.ID || d.version != connection.Version {
		*d = taskDestinations{connectionID: connection.ID, version: connection.Version}
		if connection.SourceKind == obsidianTasks {
			m.loadNewTaskNotes(d)
		}
	}
	if connection.SourceKind == common.WorkbenchGitHubRepository || connection.SourceKind == managedGitHub {
		ui.Field(c.Key("body-field"), tr("描述"), func() {
			ui.TextInput(c.Key("body"), &m.newTaskBody).Label(tr("描述")).FillWidth()
		})
		return connection.Writable
	}
	if connection.SourceKind != obsidianTasks {
		return connection.Writable
	}
	if ui.Button(c.Key("reload"), tr("刷新写入位置")).Clicked() {
		*d = taskDestinations{connectionID: connection.ID, version: connection.Version}
		m.loadNewTaskNotes(d)
	}
	if len(d.notes) == 0 {
		components.Muted(c.Key("notes-empty"), tr("范围内没有笔记"))
		return false
	}
	ui.Field(c.Key("note"), tr("选择笔记"), func() {
		if ui.Select(c, &d.note, d.notes).FillWidth().Changed() {
			d.problem = ""
			m.loadNewTaskHeadings(d)
		}
	})
	if d.note == "" {
		return false
	}
	if len(d.labels) == 0 {
		components.Muted(c.Key("headings-empty"), tr("笔记中没有可写章节"))
		return false
	}
	ui.Field(c.Key("heading"), tr("选择章节"), func() {
		ui.Select(c, &d.heading, d.labels).FillWidth()
	})
	return connection.Writable && d.byLabel[d.heading].TargetVersion != ""
}

func (m *mockup) loadNewTaskNotes(d *taskDestinations) {
	notes, err := m.live.WorkbenchNotes(d.connectionID)
	if err != nil {
		d.problem = common.ProtocolError(err).Message
		return
	}
	for _, note := range notes {
		d.notes = append(d.notes, note.NotePath)
	}
}

func (m *mockup) loadNewTaskHeadings(d *taskDestinations) {
	d.heading, d.labels = "", nil
	d.byLabel = make(map[string]common.WorkbenchHeading)
	headings, err := m.live.WorkbenchHeadings(d.connectionID, d.note)
	if err != nil {
		d.problem = common.ProtocolError(err).Message
		return
	}
	for _, heading := range headings {
		if heading.TargetVersion == "" {
			continue
		}
		label := strings.Join(heading.HeadingPath, " › ")
		if _, exists := d.byLabel[label]; exists {
			for suffix := 2; ; suffix++ {
				candidate := fmt.Sprintf("%s (%d)", label, suffix)
				if _, used := d.byLabel[candidate]; !used {
					label = candidate
					break
				}
			}
		}
		d.labels = append(d.labels, label)
		d.byLabel[label] = heading
	}
}
