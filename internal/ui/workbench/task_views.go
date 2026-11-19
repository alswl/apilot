package workbench

import (
	"maps"
	"slices"
	"strings"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/alswl/apilot/pkg/common"
	"github.com/egoist/mygo/ui"
)

type taskViewState struct {
	Query, Status, Source, Grouping string
	Sort                            ui.SortOrder
	Columns                         map[string]bool
	WatchOnly                       bool
	Page, PageSize                  int
	PageSizeOverride                int
}

type savedTaskView struct {
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	State taskViewState `json:"state"`
}

func (m *mockup) viewState() taskViewState {
	return taskViewState{Query: m.search, Status: m.statusFilter, Source: m.sourceFilter, Grouping: m.taskGrouping, Sort: m.taskSort, Columns: maps.Clone(m.taskColumnsHidden), WatchOnly: m.taskWatchOnly, Page: m.taskPageIndex, PageSize: m.taskPageSize, PageSizeOverride: m.taskPageSizeOverride}
}

func (m *mockup) restoreView(state taskViewState) {
	m.search, m.statusFilter, m.sourceFilter, m.taskGrouping = state.Query, state.Status, state.Source, state.Grouping
	m.taskWatchOnly = state.WatchOnly
	m.taskSort, m.taskColumnsHidden = state.Sort, maps.Clone(state.Columns)
	m.taskPageIndex, m.taskPageSize = state.Page, state.PageSize
	m.taskPageSizeOverride = state.PageSizeOverride
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
	state := taskViewState{WatchOnly: id == "watching", Columns: map[string]bool{"priority": true, "labels": true, "assignee": true}, Status: "全部状态", Source: "全部来源", Grouping: "不分组", Sort: ui.SortOrder{Column: "title"}}
	if draft, ok := m.taskViewDrafts[id]; ok {
		state = draft
	} else {
		for _, saved := range m.savedTaskViews {
			if saved.ID == id {
				state = saved.State
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
				items = append(items, components.ViewItem{ID: saved.ID, Title: saved.Name})
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
		columns := ui.Button(c.Key("columns"), tr("列")).Selected(m.taskColumnsOpen)
		if columns.Clicked() {
			m.taskColumnsOpen = !m.taskColumnsOpen
		}
		m.taskControlsPopover(c.Key("columns-panel"), columns, &m.taskColumnsOpen, func() {
			m.taskColumnOptions(c.Key("column-options"))
		})
		save := ui.Button(c.Key("save"), tr("保存视图"))
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
	localizedSelect(c.Key("group"), &m.taskGrouping, []string{"不分组", "按状态分组", "按来源分组"}, 132)
}

func (m *mockup) taskColumnOptions(c *ui.Context) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for i, column := range taskColumns {
			shown := i == 0 || m.taskColumnVisible(column.ID)
			if ui.Checkbox(c.Key(column.ID), &shown, tr(column.Title)).Disabled(i == 0).Changed() && i != 0 {
				if m.taskColumnsHidden == nil {
					m.taskColumnsHidden = make(map[string]bool)
				}
				m.taskColumnsHidden[column.ID] = !shown
			}
		}
	})
}

func (m *mockup) openSaveTaskView() {
	m.saveTaskViewOpen, m.taskViewName, m.taskViewSaveError = true, "", false
}

func (m *mockup) savedTaskViewDialog(c *ui.Context) {
	ui.Modal(c, &m.saveTaskViewOpen, func() {
		ui.Column(c).Width(380).Gap(12).Children(func() {
			components.Title(c, tr("保存视图"))
			input := ui.TextInput(c.Key("name"), &m.taskViewName).Label(tr("视图名称")).Placeholder(tr("视图名称")).AutoFocus()
			name := strings.TrimSpace(m.taskViewName)
			if (input.Submitted() || ui.PrimaryButton(c.Key("save"), tr("保存")).Disabled(name == "").Clicked()) && name != "" {
				state := m.viewState()
				state.Page = 0
				saved := savedTaskView{ID: common.ID(), Name: name, State: state}
				if err := m.saveNamedTaskView(saved); err != nil {
					m.taskViewSaveError = true
					return
				}
				m.selectTaskView(saved.ID)
				m.saveTaskViewOpen = false
			}
			if m.taskViewSaveError {
				components.Muted(c.Key("error"), tr("无法保存视图"))
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

func (m *mockup) taskColumnVisible(id string) bool {
	if hidden, chosen := m.taskColumnsHidden[id]; chosen {
		return !hidden
	}
	return m.layoutShowsTaskColumn(id)
}

func (m *mockup) visibleColumns() ([]ui.TableColumn, []int) {
	var columns []ui.TableColumn
	var indexes []int
	for i, column := range taskColumns {
		if !m.taskColumnVisible(column.ID) {
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
			ID: o.ID, Title: m.sourceText(o.Title), Cells: cellsAt(m.taskValues(o), indexes), Icons: cellsAt(icons, indexes),
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
