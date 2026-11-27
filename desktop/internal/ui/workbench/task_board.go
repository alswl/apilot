package workbench

import (
	"fmt"
	"strconv"

	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

const taskBoardColumnWidth = 224
const taskBoardColumnGap = 12

var taskBoardStates = [...]string{"open", "in_progress", "done", "cancelled", "unknown"}

func (m *mockup) taskLayoutControl(c *ui.Context) {
	selected := 0
	if m.taskBoard {
		selected = 1
	}
	segments := ui.SegmentedBase(c, &selected, 2)
	theme := c.Theme()
	segments.Track.Label(tr("任务布局")).Padding(2).Gap(2).Radius(6).Border(1, theme.Border).Children(func() {
		for i, label := range []string{tr("列表"), tr("看板")} {
			segment := segments.Segment(i).Label(label).Padding(3, 8).MinHeight(24).Radius(4)
			if i == selected {
				segment.Background(theme.Selection)
			} else if segment.Hovered() {
				segment.Background(theme.SurfaceHover)
			}
			segment.Children(func() { components.DataText(c, label).TextColor(theme.Text).SingleLine() })
		}
	})
	if segments.Track.Changed() {
		m.taskBoard = selected == 1
		m.customQuery.page = 0
		m.taskDisplayOpen = false
		m.closeInspector()
		m.restoreTaskFocus = false
		c.Invalidate()
	}
}

func (m *mockup) taskBoardColumns(rows []int) [len(taskBoardStates)][]int {
	var columns [len(taskBoardStates)][]int
	for _, row := range rows {
		column := len(columns) - 1
		for i, state := range taskBoardStates {
			if m.objects[row].State == state {
				column = i
				break
			}
		}
		columns[column] = append(columns[column], row)
	}
	return columns
}

func (m *mockup) taskNavigationRows() []int {
	rows := m.taskRows()
	if !m.taskBoard || m.activeTaskQuery() != nil {
		return rows
	}
	columns := m.taskBoardColumns(rows)
	ordered := make([]int, 0, len(rows))
	for _, column := range columns {
		ordered = append(ordered, column...)
	}
	return ordered
}

func (m *mockup) taskBoardSort(c *ui.Context) {
	var options []string
	selected := taskColumns[0].Title
	for _, column := range taskColumns {
		if column.Sortable {
			options = append(options, column.Title)
		}
		if column.ID == m.taskSort.Column {
			selected = column.Title
		}
	}
	previous := selected
	localizedSelect(c.Key("field"), &selected, options, 138).Label(tr("排序字段"))
	if selected != previous {
		for _, column := range taskColumns {
			if selected == column.Title {
				m.taskSort = ui.SortOrder{Column: column.ID}
				break
			}
		}
		m.taskBoardScroll.Y = 0
	}
	label, symbol := tr("升序"), "arrow.up"
	if m.taskSort.Descending {
		label, symbol = tr("降序"), "arrow.down"
	}
	if components.IconToggle(c.Key("direction"), symbol, label, m.taskSort.Descending).Clicked() {
		m.taskSort.Descending = !m.taskSort.Descending
		m.taskBoardScroll.Y = 0
	}
}

func (m *mockup) taskBoardResults(c *ui.Context, rows []int) {
	width := float32(len(taskBoardStates)*taskBoardColumnWidth + (len(taskBoardStates)-1)*taskBoardColumnGap)
	ui.Row(c.Key("toolbar")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		components.Muted(c.Key("sort-label"), tr("排序"))
		m.taskBoardSort(c.Key("sort"))
		ui.Spacer(c).Grow(1)
		total := fmt.Sprintf(tr("共 %d 项"), len(rows))
		if len(rows) == 1 {
			total = tr("共 1 项")
		}
		components.Muted(c.Key("total"), total)
		// Provide click targets as well as two-axis scrolling for trackpads.
		if m.layout.listWidth-24 < width {
			step := float32(taskBoardColumnWidth + taskBoardColumnGap)
			scroll := &m.taskBoardScroll
			if components.IconButton(c.Key("left"), "chevron.left", tr("向左浏览看板"), scroll.X <= 0).Clicked() {
				scroll.X = max(0, scroll.X-step)
				c.Invalidate()
			}
			if components.IconButton(c.Key("right"), "chevron.right", tr("向右浏览看板"), scroll.MaxX > 0 && scroll.X >= scroll.MaxX).Clicked() {
				scroll.X = min(scroll.MaxX, scroll.X+step)
				c.Invalidate()
			}
		}
	})
	columns := m.taskBoardColumns(rows)
	if m.taskSelection != "" {
		found := false
		for _, row := range rows {
			if m.objects[row].ID == m.taskSelection {
				found = true
				break
			}
		}
		if !found {
			m.taskSelection = ""
		}
	}
	ui.ScrollBoth(c.Key("scroll")).Grow(1).MinHeight(0).MinWidth(0).FillWidth().TrackScroll(&m.taskBoardScroll).Children(func() {
		ui.Row(c.Key("columns")).Width(width).AlignItems(ui.Start).Gap(taskBoardColumnGap).Children(func() {
			for i, state := range taskBoardStates {
				ui.Column(c.Key(state)).Width(taskBoardColumnWidth).Shrink(0).Padding(8).Gap(8).
					Radius(8).Background(c.Theme().Background).Children(func() {
					ui.Row(c.Key("header")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
						components.Heading(c.Key("state"), tr(workbenchStateLabel(state)))
						ui.Spacer(c).Grow(1)
						components.Muted(c.Key("count"), strconv.Itoa(len(columns[i])))
					})
					for _, row := range columns[i] {
						m.taskBoardCard(c.Key(m.objects[row].ID), row)
					}
				})
			}
		})
	})
}

func (m *mockup) taskBoardCard(c *ui.Context, index int) {
	task := m.objects[index]
	title, source := m.sourceText(task.Title), m.taskSourceDescription(task)
	priority, due := taskDisplayPriority(task), taskDueValue(task)
	label := title + " · " + source
	if priority != "" {
		label += " · " + tr("优先级") + " " + taskPriorityLabel(priority)
	}
	if due != "" {
		label += " · " + tr("截止日") + " " + tr(due)
	}
	selected := task.ID == m.taskSelection
	theme := c.Theme()
	card := ui.ButtonBase(c).Label(label).Tooltip(label).FillWidth().MinWidth(0).Padding(10).
		Radius(6).Border(1, theme.Border).Selected(selected).Justify(ui.Start)
	background := theme.Surface
	if selected {
		background = theme.Selection
	} else if card.Hovered() {
		background = theme.SurfaceHover
	}
	card.Background(background).Children(func() {
		ui.Column(c).FillWidth().MinWidth(0).Gap(4).Children(func() {
			components.DataText(c.Key("title"), title).TextColor(theme.Text).FontWeight(600).SingleLine().Ellipsis("…").MinWidth(0)
			components.DataText(c.Key("source"), source).TextColor(theme.TextMuted).SingleLine().Ellipsis("…").MinWidth(0)
			if priority != "" || due != "" {
				ui.Row(c.Key("fields")).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
					if priority != "" {
						components.Muted(c.Key("priority"), tr("优先级")+" "+taskPriorityLabel(priority))
					}
					if due != "" {
						components.Muted(c.Key("due"), tr(due))
					}
				})
			}
		})
	})
	if selected && m.restoreTaskFocus {
		card.Focus()
		m.restoreTaskFocus = false
		if m.layout.revealSelection {
			card.ScrollIntoView()
		}
		m.layout.revealSelection = false
	}
	if card.Clicked() {
		m.taskSelection = task.ID
		m.openFullTask(index)
		m.layout.revealSelection = false
		c.Invalidate()
	}
}
