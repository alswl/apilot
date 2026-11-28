package workbench

import (
	"maps"
	"slices"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type taskViewState struct {
	Query, Status, Source, Grouping string
	SourceID                        string `json:"source_id,omitempty"`
	Sort                            ui.SortOrder
	Columns                         map[string]bool
	TableLayout                     ui.TableLayout
	WatchOnly                       bool
	Page, PageSize                  int
	PageSizeOverride                int
	Board                           bool `json:"board,omitempty"`
}

type savedTaskView struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	State taskViewState  `json:"state"`
	Query *taskQueryView `json:"query,omitempty"`
}

func (m *mockup) viewState() taskViewState {
	source, sourceID := m.taskViewSource()
	return taskViewState{Query: m.search, Status: m.statusFilter, Source: source, SourceID: sourceID, Grouping: m.taskGrouping, Sort: m.taskSort, Columns: maps.Clone(m.taskColumnsHidden), TableLayout: cloneTaskTableLayout(m.taskTable.Columns), WatchOnly: m.taskWatchOnly, Page: m.taskPageIndex, PageSize: m.taskPageSize, PageSizeOverride: m.taskPageSizeOverride, Board: m.taskBoard}
}

func (m *mockup) restoreView(state taskViewState) {
	m.search, m.statusFilter, m.sourceFilter, m.taskGrouping = state.Query, state.Status, m.resolveTaskViewSource(state.Source, state.SourceID), state.Grouping
	m.sourceFilterID = state.SourceID
	m.taskWatchOnly = state.WatchOnly
	m.taskBoard = state.Board && m.unifiedTasks()
	m.taskBoardScroll = ui.ScrollState{}
	m.taskSort, m.taskColumnsHidden = state.Sort, maps.Clone(state.Columns)
	m.taskTable.Columns = cloneTaskTableLayout(state.TableLayout)
	m.taskPageIndex, m.taskPageSize = state.Page, state.PageSize
	m.taskPageSizeOverride = state.PageSizeOverride
	if m.taskPageSize == 0 {
		m.taskPageSize = defaultTaskPageSize
	}
	m.taskPageCriteria = m.paginationCriteria()
	m.pageScroll[tasksPage].ScrollTo(0, 0, ui.Animation{})
}

const unavailableTaskSourceID = "\x00source-id:"
const unavailableTaskSourceName = "\x00source-name:"

func (m *mockup) taskViewSource() (string, string) {
	if id, ok := strings.CutPrefix(m.sourceFilter, unavailableTaskSourceID); ok {
		return "", id
	}
	if name, ok := strings.CutPrefix(m.sourceFilter, unavailableTaskSourceName); ok {
		return name, ""
	}
	if m.sourceFilter == "" || m.sourceFilter == "全部来源" {
		return m.sourceFilter, ""
	}
	if m.sourceFilterID != "" {
		if conn := m.connectionByID(m.sourceFilterID); conn != nil && conn.Name == m.sourceFilter {
			return m.sourceFilter, m.sourceFilterID
		}
	}
	var id string
	for _, conn := range m.connectionsData {
		if conn.Name != m.sourceFilter {
			continue
		}
		if id != "" {
			return m.sourceFilter, ""
		}
		id = conn.ID
	}
	return m.sourceFilter, id
}

func (m *mockup) matchesTaskSource(connectionID string) bool {
	if m.sourceFilter == "全部来源" {
		return true
	}
	if m.sourceFilterID != "" {
		if conn := m.connectionByID(m.sourceFilterID); conn != nil && conn.Name == m.sourceFilter {
			return connectionID == m.sourceFilterID
		}
		return false
	}
	return m.sourceFilter == m.connectionName(connectionID)
}

func (m *mockup) resolveTaskViewSource(name, id string) string {
	if id != "" {
		if conn := m.connectionByID(id); conn != nil {
			return conn.Name
		}
		return unavailableTaskSourceID + id
	}
	if name == "" || name == "全部来源" {
		return name
	}
	count := 0
	for _, conn := range m.connectionsData {
		if conn.Name == name {
			count++
		}
	}
	if count == 1 {
		return name
	}
	return unavailableTaskSourceName + name
}

func (m *mockup) taskSourceUnavailable() bool {
	return strings.HasPrefix(m.sourceFilter, unavailableTaskSourceID) || strings.HasPrefix(m.sourceFilter, unavailableTaskSourceName)
}

func cloneTaskTableLayout(layout ui.TableLayout) ui.TableLayout {
	return ui.TableLayout{Order: slices.Clone(layout.Order), Widths: maps.Clone(layout.Widths)}
}

func defaultTaskViewState(id string) taskViewState {
	state := taskViewState{WatchOnly: id == "watching", Columns: map[string]bool{"priority": true, "labels": true, "assignee": true}, Status: "全部状态", Source: "全部来源", Grouping: "不分组", Sort: ui.SortOrder{Column: "title"}}
	switch id {
	case "in-progress":
		state.Status = "进行中"
	case "pending":
		state.Status = "待处理"
	}
	return state
}

func (m *mockup) taskViewBaseline() taskViewState {
	if saved := m.currentSavedView(); saved != nil {
		return saved.State
	}
	return defaultTaskViewState(m.taskView)
}

func (m *mockup) taskViewChanged() bool {
	baseline := m.taskViewBaseline()
	current := m.viewState()
	sourceChanged := current.SourceID != baseline.SourceID || (current.SourceID == "" && current.Source != baseline.Source)
	return current.Query != baseline.Query || current.Status != baseline.Status || sourceChanged ||
		current.Grouping != baseline.Grouping || current.Sort != baseline.Sort || current.WatchOnly != baseline.WatchOnly ||
		current.Board != baseline.Board || current.PageSizeOverride != baseline.PageSizeOverride || !maps.Equal(current.Columns, baseline.Columns) ||
		!slices.Equal(current.TableLayout.Order, baseline.TableLayout.Order) || !maps.Equal(current.TableLayout.Widths, baseline.TableLayout.Widths)
}

func (m *mockup) selectTaskView(id string) {
	if m.taskView == id {
		return
	}
	m.rememberQueryBrowsing()
	m.taskViewDrafts[m.taskView] = m.viewState()
	m.taskView = id
	m.customQuery.revealTab = true
	state := defaultTaskViewState(id)
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
	m.restoreQueryBrowsing()
	m.rememberLocation()
}

func (m *mockup) taskViewBar(c *ui.Context) {
	compact := m.compactTaskList()
	narrow := m.customQuery.path != "" && m.layout.listWidth > 0 && m.layout.listWidth < 620
	if narrow {
		ui.Column(c.Key("stack")).FillWidth().MinWidth(0).Gap(4).Children(func() {
			m.taskViewStrip(c.Key("views"))
			ui.Row(c.Key("actions")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
				ui.Spacer(c).Grow(1)
				m.taskViewActions(c, compact)
			})
		})
	} else {
		ui.Row(c.Key("bar")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Column(c.Key("view-list")).Grow(1).MinWidth(0).Children(func() {
				m.taskViewStrip(c.Key("views"))
			})
			m.taskViewActions(c, compact)
		})
	}
	if m.activeTaskQuery() == nil && m.customQuery.problem != "" {
		components.Muted(c.Key("config-problem"), m.customQuery.problem)
	}
	if m.taskViewSaveError && !m.saveTaskViewOpen {
		components.Muted(c.Key("save-error"), tr("无法保存视图"))
	}
	ui.Divider(c)
	if m.activeTaskQuery() == nil && !m.taskBoard && m.taskDisplayOpen && !compact {
		m.taskDisplayOptions(c.Key("display-options"))
	}
}

func (m *mockup) taskViewStrip(c *ui.Context) {
	items := []components.ViewItem{{ID: "all", Title: tr("全部任务")}, {ID: "in-progress", Title: tr("处理中")}, {ID: "pending", Title: tr("待处理")}, {ID: "watching", Title: tr("关注中")}}
	for _, saved := range m.savedTaskViews {
		title := saved.Name
		if saved.ID == m.taskView && m.customQueryChanged() {
			title += " •"
		}
		items = append(items, components.ViewItem{ID: saved.ID, Title: title})
	}
	for _, id := range slices.Sorted(maps.Keys(m.customQuery.conversion)) {
		items = append(items, components.ViewItem{ID: id, Title: m.customQuery.conversion[id].view.Name + " •"})
	}
	current := m.taskView
	if current == "" {
		current = "all"
	}
	var chosen string
	if m.customQuery.path != "" {
		chosen = components.ScrollableViewStrip(c.Key("views"), items, current, m.customQuery.revealTab)
		m.customQuery.revealTab = false
	} else {
		chosen = components.ViewStrip(c.Key("views"), items, current)
	}
	if chosen != "" {
		if chosen == "all" {
			chosen = ""
		}
		m.selectTaskView(chosen)
	}
}

func (m *mockup) taskViewActions(c *ui.Context, compact bool) {
	if m.unifiedTasks() {
		m.taskLayoutControl(c.Key("layout"))
	}
	m.queryTabButtons(c.Key("query-actions"))
	if m.activeTaskQuery() != nil {
		return
	}
	if !m.taskBoard {
		display := components.IconToggle(c.Key("display"), "slider.horizontal.3", tr("显示选项"), m.taskDisplayOpen)
		if display.Clicked() {
			m.taskDisplayOpen = !m.taskDisplayOpen
		}
		if compact {
			m.taskControlsPopover(c.Key("display-panel"), display, &m.taskDisplayOpen, func() {
				m.taskDisplayOptions(c.Key("display-options"))
			})
		}
	}
	if m.customQuery.path != "" {
		return
	}
	label := "保存视图"
	current := m.currentSavedView()
	if current != nil {
		label = "更新视图"
	}
	save := ui.SplitButton(c.Key("save"), "")
	changed := m.taskViewChanged()
	save.Group.Disabled(!changed)
	save.Button.Label(tr(label)).Tooltip(tr(label)).Children(func() {
		color := c.Theme().TextMuted
		if changed {
			color = c.Theme().Text
		}
		components.Symbol(c, "bookmark", color)
	})
	save.Arrow.Label(tr("视图保存选项")).Menu(func(menu *ui.Menu) {
		if menu.Item(tr("更新视图")).Disabled(current == nil || !changed).Chosen() {
			m.saveCurrentTaskView()
		}
		if menu.Item(tr("另存为新视图…")).Disabled(!changed).Chosen() {
			m.openSaveTaskView()
		}
	})
	if shortcut := m.keyboard.display("view.save"); shortcut != "" {
		save.Button.Tooltip(tr(label) + " · " + shortcut)
	}
	if save.Button.Clicked() {
		m.saveCurrentTaskView()
	}
}

func (m *mockup) taskDisplayOptions(c *ui.Context) {
	localizedSelect(c.Key("group"), &m.taskGrouping, []string{"不分组", "按状态分组", "按来源分组"}, 132)
}

func (m *mockup) taskColumnMenu(menu *ui.Menu, _ int) {
	for i, column := range taskColumns {
		shown := m.taskColumnVisible(column.ID)
		if menu.Item(tr(column.Title)).Checked(shown).Disabled(i == 0).Chosen() && i != 0 {
			if m.taskColumnsHidden == nil {
				m.taskColumnsHidden = make(map[string]bool)
			}
			_, explicitlySaved := m.taskViewBaseline().Columns[column.ID]
			if !explicitlySaved && shown != m.defaultTaskColumnVisible(column.ID) {
				delete(m.taskColumnsHidden, column.ID)
			} else {
				m.taskColumnsHidden[column.ID] = shown
			}
		}
	}
}

func (m *mockup) currentSavedView() *savedTaskView {
	for i := range m.savedTaskViews {
		if m.savedTaskViews[i].ID == m.taskView {
			return &m.savedTaskViews[i]
		}
	}
	return nil
}

func (m *mockup) saveCurrentTaskView() {
	if m.activeTaskQuery() != nil {
		m.saveCustomQuery()
		return
	}
	if !m.taskViewChanged() {
		return
	}
	current := m.currentSavedView()
	if current == nil {
		m.openSaveTaskView()
		return
	}
	view := *current
	view.State = m.viewState()
	view.State.Page = 0
	m.taskViewSaveError = m.saveNamedTaskView(view) != nil
}

func (m *mockup) openSaveTaskView() {
	if !m.taskViewChanged() {
		return
	}
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
		return m.taskStateLabel(object)
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
	return m.defaultTaskColumnVisible(id)
}

func (m *mockup) defaultTaskColumnVisible(id string) bool {
	if id == "path" || id == "section" || (id == "file" && m.connectorDesktop()) {
		return false
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
			if width := map[string]float32{"status": 134, "priority": 76, "source": 70, "due": 112}[column.ID]; width != 0 {
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
		if o.NativeFields["due"] != "" {
			icons[4] = "calendar"
		}
		group.Rows = append(group.Rows, components.TableRow{
			ID: o.ID, Title: m.sourceText(o.Title), Cells: cellsAt(m.taskValues(o), indexes), Icons: cellsAt(icons, indexes),
			Leading: func(c *ui.Context) { m.taskStatusCheckbox(c, o) },
			RenderCell: func(c *ui.Context, column string) bool {
				if column != "source" {
					return false
				}
				kind := o.Kind
				if conn := m.taskConnection(o); conn != nil {
					kind = conn.SourceKind
				}
				label := m.taskSourceDescription(o)
				ui.Box(c).Label(label).Tooltip(label).Children(func() {
					connectorMark(c, kind, 16, c.Theme().TextMuted)
				})
				return true
			},
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
		groups = []components.TableGroup{{}}
	}
	m.taskTable.Sort = &m.taskSort
	m.taskTable.HeaderMenu = m.taskColumnMenu
	result := components.GroupedTables(c, &m.taskTable, m.taskGroupTables, columns, groups, m.taskSelection, len(rows) > 0 && m.restoreTaskFocus, m.layout.revealSelection)
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
