package workbench

import (
	"fmt"
	"slices"

	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type taskReturnPoint struct {
	taskID, selection, viewID                      string
	view                                           taskViewState
	listX, listY, detailX, detailY, boardX, boardY float32
}

func (m *mockup) followRelatedTask(index int) {
	origin := m.currentTaskIndex()
	if origin < 0 || origin == index {
		return
	}
	list := &m.pageScroll[tasksPage]
	detail := &m.assistantThreadFor(origin).scroll
	m.taskBackStack = append(m.taskBackStack, taskReturnPoint{
		taskID: m.objects[origin].ID, selection: m.taskSelection, viewID: m.taskView, view: m.viewState(),
		listX: list.X, listY: list.Y, detailX: detail.X, detailY: detail.Y,
		boardX: m.taskBoardScroll.X, boardY: m.taskBoardScroll.Y,
	})
	m.showTaskDetail(index)
}

func (m *mockup) returnToRelatedSource() {
	for len(m.taskBackStack) > 0 {
		last := len(m.taskBackStack) - 1
		point := m.taskBackStack[last]
		m.taskBackStack = m.taskBackStack[:last]
		index := slices.IndexFunc(m.objects, func(item sourceObject) bool { return item.ID == point.taskID })
		if index < 0 {
			continue
		}
		m.taskView = point.viewID
		m.restoreView(point.view)
		m.showTaskDetail(index)
		m.taskPageIndex, m.taskSelection = point.view.Page, point.selection
		m.layout.revealSelection = false
		m.pageScroll[tasksPage] = ui.ScrollState{X: point.listX, Y: point.listY}
		m.taskBoardScroll = ui.ScrollState{X: point.boardX, Y: point.boardY}
		m.assistantThreadFor(index).scroll = ui.ScrollState{X: point.detailX, Y: point.detailY}
		return
	}
}

func (m *mockup) taskBackLink(c *ui.Context) {
	if len(m.taskBackStack) == 0 {
		return
	}
	point := m.taskBackStack[len(m.taskBackStack)-1]
	index := slices.IndexFunc(m.objects, func(item sourceObject) bool { return item.ID == point.taskID })
	if index < 0 {
		return
	}
	label := fmt.Sprintf(tr("返回 %s"), m.sourceText(m.objects[index].Title))
	link := ui.ButtonBase(c).Label(label).Tooltip(label).MinWidth(0).MinHeight(26).Justify(ui.Start)
	link.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			components.Symbol(c, "chevron.left", c.Theme().Accent)
			components.DataText(c, label).TextColor(c.Theme().Accent).SingleLine().Ellipsis("…").MinWidth(0)
		})
	})
	if link.Clicked() {
		m.returnRelatedPending = true
		c.Invalidate()
	}
}

// leaveTaskDetail returns to the list and restores its previous visibility.
func (m *mockup) leaveTaskDetail() {
	m.layout.detailExpanded = false
	if m.layout.detailReturnHidden {
		m.layout.detailHidden = true
	}
	m.closeInspector()
}

func (m *mockup) showTaskList() {
	m.leaveTaskDetail()
	if m.unifiedTasks() {
		m.layout.detailHidden = true
	}
}
