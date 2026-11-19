package workbench

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/alswl/apilot/pkg/common"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) writableConnections() []string {
	var sources []string
	for _, conn := range m.connectionsData {
		if conn.Writable && (conn.Status == "同步正常" || conn.Status == "可用") {
			sources = append(sources, conn.Name)
		}
	}
	return sources
}

func (m *mockup) openNewTask() {
	sources := m.writableConnections()
	if len(sources) == 0 {
		return
	}
	m.newTaskOpen = true
	m.newTaskRequest = common.WorkbenchTaskWrite{}
	m.newTaskTargets = taskDestinations{}
	m.newTaskDraft, m.newTaskSource = "", sources[0]
}

func (m *mockup) newTaskDialog(c *ui.Context) {
	ui.Modal(c, &m.newTaskOpen, func() {
		ui.Column(c).Width(440).Gap(12).Children(func() {
			components.Title(c, tr("新建任务"))
			var submitted bool
			ui.Field(c.Key("title-field"), tr("任务名称"), func() {
				submitted = ui.TextInput(c.Key("title"), &m.newTaskDraft).Label(tr("任务名称")).
					AutoFocus().FillWidth().Submitted()
			})
			sources := m.writableConnections()
			ui.Field(c.Key("source-field"), tr("来源"), func() {
				localizedSelect(c, &m.newTaskSource, sources, 0)
			})
			validTarget := true
			if m.live != nil {
				validTarget = m.newTaskDestinationFields(c.Key("destination"))
			}
			if m.newTaskTargets.problem != "" {
				components.Muted(c, tr(m.newTaskTargets.problem))
			}
			title := strings.TrimSpace(m.newTaskDraft)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					m.newTaskOpen = false
					return
				}
				save := ui.PrimaryButton(c.Key("create"), tr("创建任务")).Disabled(title == "" || len(sources) == 0 || !validTarget)
				if (save.Clicked() || submitted) && title != "" && validTarget {
					m.createTask(title)
					c.Invalidate()
				}
			})
		})
	})
}

func (m *mockup) createTask(title string) {
	for _, conn := range m.connectionsData {
		if conn.Name != m.newTaskSource || !conn.Writable || (conn.Status != "同步正常" && conn.Status != "可用") {
			continue
		}
		if m.live != nil {
			request := common.WorkbenchTaskWrite{Title: title, ConnectionID: conn.ID, ConnectionVersion: conn.Version}
			if conn.SourceKind == obsidianTasks {
				destinations := m.newTaskTargets
				heading, ok := destinations.byLabel[destinations.heading]
				if !ok || destinations.connectionID != conn.ID || heading.TargetVersion == "" {
					return
				}
				request.NotePath, request.HeadingPath, request.TargetVersion = destinations.note, heading.HeadingPath, heading.TargetVersion
			}
			prior := m.newTaskRequest
			if prior.RequestID != "" && prior.Title == request.Title && prior.ConnectionID == request.ConnectionID &&
				prior.NotePath == request.NotePath && slices.Equal(prior.HeadingPath, request.HeadingPath) {
				request = prior
			} else {
				request.RequestID = common.ID()
				m.newTaskRequest = request
			}
			receipt, err := m.live.WriteWorkbenchTask(request)
			if err != nil {
				problem := common.ProtocolError(err)
				m.newTaskTargets.problem = problem.Message
				if problem.Code == "STALE_REVISION" {
					m.newTaskRequest = common.WorkbenchTaskWrite{}
				}
				return
			}
			if receipt.SyncState != "confirmed" {
				m.newTaskTargets.problem = "来源尚未确认，重试可核对本次提交"
				if receipt.SyncState == "conflict" {
					m.newTaskTargets.problem = "来源已变化，请刷新写入位置"
					m.newTaskRequest = common.WorkbenchTaskWrite{}
				}
				return
			}
			m.newTaskOpen = false
			m.liveGeneration++
			m.navigate(tasksPage)
			m.refreshLive()
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
		m.restoreTaskFocus = false
		m.recordTaskEvent(id, "在模拟来源创建了任务")
		m.openFullTask(len(m.objects) - 1)
		return
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
		if m.connectionsData[i].Name == m.newTaskSource {
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
