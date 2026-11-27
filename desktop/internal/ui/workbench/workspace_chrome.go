package workbench

import (
	"fmt"
	"slices"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

const statusBarHeight = 30

func (m *mockup) currentTaskIndex() int {
	if m.inspector {
		return m.selected
	}
	if m.page == tasksPage {
		return slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == m.taskSelection })
	}
	return -1
}

func (m *mockup) workspaceBreadcrumb(c *ui.Context) {
	ui.Row(c).Grow(1).MinWidth(0).FillHeight().Gap(8).AlignItems(ui.Center).DragWindow().Children(func() {
		if breadcrumbButton(c.Key("home"), "Apilot", tr("返回任务工作区")) {
			m.navigate(tasksPage)
			m.showTaskList()
			return
		}
		components.Symbol(c.Key("page-chevron"), "chevron.right", c.Theme().TextMuted)
		page := tr(pages[m.page])
		current := ""
		var back func()
		switch m.page {
		case nowPage:
			if m.adaptiveLayout() {
				if m.nowLibraryOpen {
					current, back = tr("所有会话"), func() { m.navigate(nowPage) }
				} else {
					current, back = tr(m.nowSession().title), m.openNowLibrary
				}
			}
		case tasksPage:
			if index := m.currentTaskIndex(); index >= 0 && !m.layout.detailHidden && (!m.taskBoard || m.layout.detailExpanded) {
				current, back = m.sourceText(m.objects[index].Title), m.showTaskList
				if m.activeTaskQuery() != nil {
					back = m.leaveTaskDetail
				}
				if len(m.taskBackStack) > 0 {
					back = func() { m.returnRelatedPending = true }
					page = m.returnTaskTitle()
				}
			}
		}
		if back != nil {
			if breadcrumbButton(c.Key("page"), page, fmt.Sprintf(tr("返回 %s"), page)) {
				back()
				return
			}
			components.Symbol(c.Key("item-chevron"), "chevron.right", c.Theme().TextMuted)
			page = current
		}
		components.DataText(c.Key("current"), page).SingleLine().Ellipsis("…").Tooltip(page).MinWidth(0)
	})
}

func breadcrumbButton(c *ui.Context, title, label string) bool {
	button := ui.ButtonBase(c).Label(label).Tooltip(label).MinHeight(28).Padding(2, 4).Radius(4)
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() { components.DataText(c, title).SingleLine().FontWeight(600) })
	return button.Clicked()
}

func (m *mockup) statusBar(c *ui.Context) {
	total, pending, unhealthy, awaiting := len(m.connectionsData), len(m.pendingSync), 0, 0
	width, _ := c.Size()
	for _, conn := range m.connectionsData {
		if conn.SourceKind == common.WorkbenchLocal {
			total--
			continue
		}
		if m.pendingSync[conn.ID] {
			continue
		}
		switch conn.Status {
		case "未接入":
			total--
		case "等待首次同步":
			awaiting++
		case "同步正常":
		default:
			unhealthy++
		}
	}
	activeRuns := 0
	for _, task := range m.automation.tasks {
		if task.LastRun != nil && task.LastRun.Active() {
			activeRuns++
		}
	}
	if m.hostPanels.RunningAutomation != nil {
		activeRuns = m.hostPanels.RunningAutomation()
	}
	ui.Row(c).FillWidth().Height(statusBarHeight).Shrink(0).Padding(0, 12).Gap(6).AlignItems(ui.Center).
		Background(c.Theme().Background).Children(func() {
		pendingTasks, workingTasks := 0, 0
		for _, task := range m.objects {
			switch task.State {
			case "open":
				pendingTasks++
			case "in_progress":
				workingTasks++
			case "":
				switch task.NativeFields["status"] {
				case "待处理":
					pendingTasks++
				case "进行中":
					workingTasks++
				}
			}
		}
		if statusCount(c.Key("tasks"), "list.bullet", fmt.Sprintf(tr("%d 项任务"), len(m.objects)), fmt.Sprintf(tr("%d 项任务"), len(m.objects)), c.Theme().TextMuted) {
			m.navigate(tasksPage)
			m.selectTaskView("")
		}
		for _, taskStatus := range []struct {
			id, icon, label string
			count           int
		}{
			{"pending", "circle", "待处理 %d", pendingTasks},
			{"in-progress", "circle.lefthalf.filled", "处理中 %d", workingTasks},
		} {
			label := fmt.Sprintf(tr(taskStatus.label), taskStatus.count)
			if statusCount(c.Key(taskStatus.id), taskStatus.icon, label, label, c.Theme().TextMuted) {
				m.navigate(tasksPage)
				m.selectTaskView(taskStatus.id)
			}
		}
		if m.liveStale {
			components.StatusLabel(c.Key("live-stale"), components.Status{Text: tr("数据可能已过时"), Icon: "exclamationmark.circle", Color: c.Theme().Warning})
		}
		ui.Spacer(c).Grow(1).MinWidth(0)
		for _, item := range []struct {
			icon, description, short string
			count                    int
			color                    ui.Color
		}{
			{"checkmark.circle", "%d 个来源已同步", "已同步 %d", total - pending - unhealthy - awaiting, c.Theme().Success},
			{"arrow.triangle.2.circlepath", "正在同步 %d 个来源", "同步中 %d", pending, c.Theme().TextMuted},
			{"exclamationmark.circle", "%d 个来源待重连", "异常 %d", unhealthy, c.Theme().Warning},
			{"clock", "%d 个来源待同步", "待同步 %d", awaiting, c.Theme().TextMuted},
		} {
			value := fmt.Sprint(item.count)
			if width >= 900 {
				value = fmt.Sprintf(tr(item.short), item.count)
			}
			if item.count > 0 && statusCount(c.Key(item.icon), item.icon, fmt.Sprintf(tr(item.description), item.count), value, item.color) {
				m.navigate(connectionsPage)
			}
		}
		runCount := fmt.Sprint(activeRuns)
		if width >= 900 {
			runCount = fmt.Sprintf(tr("运行中 %d"), activeRuns)
		}
		if activeRuns > 0 && statusCount(c.Key("runs"), "play.circle", fmt.Sprintf(tr("%d 项运行中"), activeRuns), runCount, c.Theme().TextMuted) {
			m.navigate(scheduledPage)
		}
		mode := "本地模拟"
		if m.framework {
			mode = "本地工作台"
		}
		ui.Box(c.Key("mode")).Tooltip(tr(mode)).Label(tr(mode)).Children(func() {
			components.Symbol(c, "laptopcomputer", c.Theme().TextMuted)
		})
	})
}

func statusCount(c *ui.Context, icon, label, value string, color ui.Color) bool {
	button := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(2, 4).MinHeight(24)
	button.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			components.Symbol(c, icon, color)
			components.Muted(c, value)
		})
	})
	return button.Clicked()
}

func (m *mockup) assistantDialog(c *ui.Context) {
	if !m.assistantOpen {
		return
	}
	ui.Modal(c, &m.assistantOpen, func() {
		ui.Column(c).Width(558).Height(430).Gap(12).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				components.Title(c, tr("问助理"))
				ui.Spacer(c).Grow(1)
				if components.IconButton(c.Key("close"), "xmark", tr("关闭助理"), false).Clicked() {
					m.assistantOpen = false
				}
			})
			if m.assistantContext >= 0 {
				m.assistantView(c.Key("context"))
			} else {
				m.nowConversation(c.Key("conversation"), &m.assistantScroll, m.assistantNeedsFocus)
				m.assistantNeedsFocus = false
			}
		})
	})
}
