package workbench

import (
	"fmt"
	"slices"

	"github.com/alswl/apilot/internal/ui/components"
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
			if index := m.currentTaskIndex(); index >= 0 && !m.layout.detailHidden {
				current, back = m.sourceText(m.objects[index].Title), m.showTaskList
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
	for _, conn := range m.connectionsData {
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
	ui.Row(c).FillWidth().Height(statusBarHeight).Shrink(0).Padding(0, 12).Gap(12).AlignItems(ui.Center).
		Background(c.Theme().Background).Children(func() {
		for _, item := range []struct {
			icon, description string
			count             int
			color             ui.Color
		}{
			{"checkmark.circle", "%d 个来源已同步", total - pending - unhealthy - awaiting, c.Theme().Success},
			{"arrow.triangle.2.circlepath", "正在同步 %d 个来源", pending, c.Theme().TextMuted},
			{"exclamationmark.circle", "%d 个来源待重连", unhealthy, c.Theme().Warning},
			{"clock", "%d 个来源待同步", awaiting, c.Theme().TextMuted},
		} {
			if item.count > 0 && statusCount(c.Key(item.icon), item.icon, fmt.Sprintf(tr(item.description), item.count), item.count, item.color) {
				m.navigate(connectionsPage)
			}
		}
		ui.Spacer(c).Grow(1).MinWidth(0)
		if activeRuns > 0 && statusCount(c.Key("runs"), "play.circle", fmt.Sprintf(tr("%d 项运行中"), activeRuns), activeRuns, c.Theme().TextMuted) {
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

func statusCount(c *ui.Context, icon, label string, count int, color ui.Color) bool {
	button := ui.ButtonBase(c).Label(label).Tooltip(label).Padding(2, 4).MinHeight(24)
	button.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			components.Symbol(c, icon, color)
			components.Muted(c, fmt.Sprint(count))
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
