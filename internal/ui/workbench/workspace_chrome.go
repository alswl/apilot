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
		ui.Text(c, "Apilot").Bold().FontSize(14).Shrink(0)
		path := []string{tr(pages[m.page])}
		if m.page == nowPage && m.adaptiveLayout() {
			path = append(path, tr(m.nowSession().title))
		}
		if index := m.currentTaskIndex(); m.page == tasksPage && index >= 0 {
			path = append(path, tr(m.objects[index].Title))
		}
		for i, part := range path {
			components.Symbol(c.Key(i), "chevron.right", c.Theme().TextMuted)
			components.DataText(c.Key(part), part).SingleLine().Ellipsis("…").Tooltip(part).MinWidth(0)
		}
	})
}

func (m *mockup) statusBar(c *ui.Context) {
	total, pending, unhealthy, awaiting := len(m.connectionsData), len(m.pendingSync), 0, 0
	for _, conn := range m.connectionsData {
		if m.pendingSync[conn.ID] {
			continue
		}
		switch conn.Status {
		case "等待首次同步":
			awaiting++
		case "同步正常":
		default:
			unhealthy++
		}
	}
	syncStatus, icon, color := fmt.Sprintf(tr("%d 个来源已同步"), total), "checkmark.circle", c.Theme().Success
	switch {
	case unhealthy > 0:
		syncStatus, icon, color = fmt.Sprintf(tr("%d 个来源待重连"), unhealthy), "exclamationmark.circle", c.Theme().Warning
	case pending > 0:
		syncStatus, icon, color = fmt.Sprintf(tr("正在同步 %d 个来源"), pending), "arrow.triangle.2.circlepath", c.Theme().TextMuted
	case awaiting > 0:
		syncStatus, icon, color = fmt.Sprintf(tr("%d 个来源待同步"), awaiting), "clock", c.Theme().TextMuted
	}
	activeRuns := 0
	for _, task := range m.automation.tasks {
		if task.LastRun != nil && task.LastRun.Active() {
			activeRuns++
		}
	}
	ui.Row(c).FillWidth().Height(statusBarHeight).Shrink(0).Padding(0, 12).Gap(12).AlignItems(ui.Center).
		Background(c.Theme().Background).Children(func() {
		link := ui.ButtonBase(c.Key("sync")).Label(syncStatus).Tooltip(tr("查看连接状态")).Padding(2, 4).MinHeight(24)
		link.Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				components.Symbol(c, icon, color)
				components.Muted(c, syncStatus)
			})
		})
		if link.Clicked() {
			m.navigate(connectionsPage)
		}
		for _, conn := range m.connectionsData {
			components.DataText(c.Key(conn.ID), tr(conn.ConnectorID)+" · "+tr(conn.LastConfirmedAt)).
				TextColor(c.Theme().TextMuted).FontSize(11).SingleLine().Ellipsis("…").MinWidth(0)
		}
		ui.Spacer(c).Grow(1).MinWidth(0)
		if activeRuns > 0 {
			components.Muted(c, fmt.Sprintf(tr("%d 项运行中"), activeRuns))
		}
		components.Muted(c, tr("本地模拟"))
	})
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
