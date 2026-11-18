package workbench

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) writableConnections() []string {
	var sources []string
	for _, conn := range m.connectionsData {
		if conn.Writable && conn.Status == "同步正常" {
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
			title := strings.TrimSpace(m.newTaskDraft)
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					m.newTaskOpen = false
					return
				}
				save := ui.PrimaryButton(c.Key("create"), tr("创建任务")).Disabled(title == "" || len(sources) == 0)
				if (save.Clicked() || submitted) && title != "" {
					m.createTask(title)
					c.Invalidate()
				}
			})
		})
	})
}

func (m *mockup) createTask(title string) {
	for _, conn := range m.connectionsData {
		if conn.Name != m.newTaskSource || !conn.Writable || conn.Status != "同步正常" {
			continue
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
