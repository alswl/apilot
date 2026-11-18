package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type taskViewState struct {
	query, status, source, grouping string
	sort                            ui.SortOrder
	columns                         map[string]bool
	watchOnly                       bool
	page, pageSize                  int
	pageSizeOverride                int
}

type savedTaskView struct {
	id, name string
	state    taskViewState
}

func (m *mockup) viewState() taskViewState {
	return taskViewState{query: m.search, status: m.statusFilter, source: m.sourceFilter, grouping: m.taskGrouping, sort: m.taskSort, columns: maps.Clone(m.taskColumnsHidden), watchOnly: m.taskWatchOnly, page: m.taskPageIndex, pageSize: m.taskPageSize, pageSizeOverride: m.taskPageSizeOverride}
}

func (m *mockup) restoreView(state taskViewState) {
	m.search, m.statusFilter, m.sourceFilter, m.taskGrouping = state.query, state.status, state.source, state.grouping
	m.taskWatchOnly = state.watchOnly
	m.taskSort, m.taskColumnsHidden = state.sort, maps.Clone(state.columns)
	m.taskPageIndex, m.taskPageSize = state.page, state.pageSize
	m.taskPageSizeOverride = state.pageSizeOverride
	if m.taskPageSize == 0 {
		m.taskPageSize = defaultTaskPageSize
	}
	m.taskPageCriteria = m.paginationCriteria()
	m.pageScroll[tasksPage].ScrollTo(0, 0, ui.Animation{})
}

func (m *mockup) selectTaskView(id string) {
	if m.taskView == id {
		return
	}
	m.taskViewDrafts[m.taskView] = m.viewState()
	m.taskView = id
	state := taskViewState{watchOnly: id == "watching", columns: map[string]bool{"priority": true, "labels": true, "assignee": true}, status: "全部状态", source: "全部来源", grouping: "不分组", sort: ui.SortOrder{Column: "title"}}
	if draft, ok := m.taskViewDrafts[id]; ok {
		state = draft
	} else {
		for _, saved := range m.savedTaskViews {
			if saved.id == id {
				state = saved.state
				break
			}
		}
	}
	m.restoreView(state)
	m.closeInspector()
}

func (m *mockup) taskViewBar(c *ui.Context) {
	compact := m.compactTaskList()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Column(c.Key("view-list")).Grow(1).MinWidth(0).Children(func() {
			items := []components.ViewItem{{ID: "all", Title: tr("全部任务")}, {ID: "watching", Title: tr("关注中")}}
			for _, saved := range m.savedTaskViews {
				items = append(items, components.ViewItem{ID: saved.id, Title: saved.name})
			}
			current := m.taskView
			if current == "" {
				current = "all"
			}
			if chosen := components.ViewStrip(c.Key("views"), items, current); chosen != "" {
				if chosen == "all" {
					chosen = ""
				}
				m.selectTaskView(chosen)
			}
		})
		display := components.IconToggle(c.Key("display"), "slider.horizontal.3", tr("显示选项"), m.taskDisplayOpen)
		if display.Clicked() {
			m.taskDisplayOpen = !m.taskDisplayOpen
		}
		if compact {
			m.taskControlsPopover(c.Key("display-panel"), display, &m.taskDisplayOpen, func() {
				m.taskDisplayOptions(c.Key("display-options"))
			})
		}
		save := components.IconButton(c.Key("save"), "bookmark", tr("保存视图"), false)
		if shortcut := m.keyboard.display("view.save"); shortcut != "" {
			save.Tooltip(tr("保存视图") + " · " + shortcut)
		}
		if save.Clicked() {
			m.openSaveTaskView()
		}
	})
	ui.Divider(c)
	if m.taskDisplayOpen && !compact {
		m.taskDisplayOptions(c.Key("display-options"))
	}
}

func (m *mockup) taskDisplayOptions(c *ui.Context) {
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
		localizedSelect(c.Key("group"), &m.taskGrouping, []string{"不分组", "按状态分组", "按来源分组"}, 132)
		for _, column := range taskColumns[1:] {
			shown := !m.taskColumnsHidden[column.ID]
			if ui.Checkbox(c.Key(column.ID), &shown, tr(column.Title)).Changed() {
				if m.taskColumnsHidden == nil {
					m.taskColumnsHidden = make(map[string]bool)
				}
				m.taskColumnsHidden[column.ID] = !shown
			}
		}
	})
}

func (m *mockup) openSaveTaskView() {
	m.saveTaskViewOpen, m.taskViewName = true, ""
}

func (m *mockup) savedTaskViewDialog(c *ui.Context) {
	ui.Modal(c, &m.saveTaskViewOpen, func() {
		ui.Column(c).Width(380).Gap(12).Children(func() {
			components.Title(c, tr("保存视图"))
			input := ui.TextInput(c.Key("name"), &m.taskViewName).Label(tr("视图名称")).Placeholder(tr("视图名称")).AutoFocus()
			name := strings.TrimSpace(m.taskViewName)
			if (input.Submitted() || ui.PrimaryButton(c.Key("save"), tr("保存")).Disabled(name == "").Clicked()) && name != "" {
				id := fmt.Sprintf("view-%d", len(m.savedTaskViews)+1)
				state := m.viewState()
				state.page = 0
				m.savedTaskViews = append(m.savedTaskViews, savedTaskView{id, name, state})
				m.selectTaskView(id)
				m.saveTaskViewOpen = false
			}
		})
	})
}

func (m *mockup) taskGroup(object sourceObject) string {
	switch m.taskGrouping {
	case "按状态分组":
		return object.NativeFields["status"]
	case "按来源分组":
		return m.connectionName(object.ConnectionID)
	default:
		return ""
	}
}

func (m *mockup) visibleColumns() ([]ui.TableColumn, []int) {
	var columns []ui.TableColumn
	var indexes []int
	for i, column := range taskColumns {
		if m.taskColumnsHidden[column.ID] || !m.layoutShowsTaskColumn(column.ID) {
			continue
		}
		column.Title = tr(column.Title)
		if englishUI.Load() {
			if width := map[string]float32{"status": 134, "priority": 76, "source": 116, "due": 112}[column.ID]; width != 0 {
				column.Width = width
			}
		}
		columns, indexes = append(columns, column), append(indexes, i)
	}
	return columns, indexes
}

func cellsAt(values []string, indexes []int) []string {
	cells := make([]string, 0, len(indexes))
	for _, i := range indexes {
		cells = append(cells, values[i])
	}
	return cells
}

func (m *mockup) taskTables(c *ui.Context, rows []int) {
	columns, indexes := m.visibleColumns()
	var groups []components.TableGroup
	for _, index := range rows {
		o := m.objects[index]
		name := m.taskGroup(o)
		if len(groups) == 0 || groups[len(groups)-1].ID != name {
			groups = append(groups, components.TableGroup{ID: name, Title: tr(name)})
		}
		group := &groups[len(groups)-1]
		icons := make([]string, len(taskColumns))
		if conn := m.taskConnection(o); conn != nil {
			icons[3] = "doc.text"
			if conn.SourceKind == githubIssues {
				icons[3] = "point.3.connected.trianglepath.dotted"
			}
		}
		if o.NativeFields["due"] != "" {
			icons[4] = "calendar"
		}
		group.Rows = append(group.Rows, components.TableRow{
			ID: o.ID, Title: tr(o.Title), Cells: cellsAt(m.taskValues(o), indexes), Icons: cellsAt(icons, indexes),
			Leading: func(c *ui.Context) { m.taskStatusCheckbox(c, o) },
			Menu: func(menu *ui.Menu) {
				if menu.Item(tr("打开完整任务")).Chosen() {
					m.openFullTask(index)
				}
				menu.Separator()
				m.taskStateMenu(o, menu)
			},
		})
	}
	if len(groups) == 0 {
		return
	}
	m.taskTable.Sort = &m.taskSort
	result := components.GroupedTables(c, &m.taskTable, m.taskGroupTables, columns, groups, m.taskSelection, m.restoreTaskFocus, m.layout.revealSelection)
	m.layout.revealSelection = false
	if m.unifiedTasks() && result.SelectedID != "" && result.SelectedID != m.taskSelection {
		m.closeInspector()
	}
	m.taskSelection = result.SelectedID
	if result.Focused {
		m.restoreTaskFocus = false
	}
	if result.ActivatedID != "" {
		index := slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == result.ActivatedID })
		if index >= 0 {
			m.openFullTask(index)
		}
	}
}
