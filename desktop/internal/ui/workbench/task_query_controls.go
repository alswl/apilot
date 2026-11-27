package workbench

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) queryTabButtons(c *ui.Context) {
	q := &m.customQuery
	if q.path == "" {
		return
	}
	if components.IconButton(c.Key("new"), "plus", tr("新视图"), false).Clicked() {
		m.queryTabAction("new")
	}
	saved := m.currentSavedView()
	actions := ui.SplitButton(c.Key("actions"), "")
	_, converting := q.conversion[m.taskView]
	active := m.activeTaskQuery()
	label := tr("更新视图")
	canSave := !q.configConflict && q.valid && m.customQueryChanged() && (!converting || q.comparison != "")
	if active == nil {
		canSave = !q.configConflict && m.taskViewChanged()
		if saved == nil {
			label = tr("保存视图")
		}
	}
	actions.Button.Label(label).Tooltip(label).Disabled(!canSave).Children(func() {
		color := c.Theme().TextMuted
		if canSave {
			color = c.Theme().Text
		}
		components.Symbol(c, "bookmark", color)
	})
	if actions.Button.Clicked() && canSave {
		m.saveCurrentTaskView()
	}
	actions.Arrow.Label(tr("视图操作")).Menu(func(menu *ui.Menu) {
		if menu.Item(tr("重命名")).Disabled(saved == nil).Chosen() {
			m.queryTabAction("rename")
		}
		for _, direction := range []struct {
			delta int
			label string
		}{{-1, tr("向左移动")}, {1, tr("向右移动")}} {
			index := slices.IndexFunc(m.savedTaskViews, func(view savedTaskView) bool { return view.ID == m.taskView })
			dest := index + direction.delta
			if menu.Item(direction.label).Disabled(index < 0 || dest < 0 || dest >= len(m.savedTaskViews)).Chosen() && index >= 0 && dest >= 0 && dest < len(m.savedTaskViews) {
				m.setTaskQueryProblem(m.updateTaskQueryConfig(func(cfg *taskQueryConfig) error {
					cfg.Views[index], cfg.Views[dest] = cfg.Views[dest], cfg.Views[index]
					return nil
				}))
			}
		}
		if menu.Item(tr("复制视图")).Disabled(saved == nil || (m.activeTaskQuery() != nil && !q.valid)).Chosen() {
			m.queryTabAction("copy")
		}
		if menu.Item(tr("删除视图")).Disabled(saved == nil).Chosen() {
			m.queryTabAction("delete")
		}
		if menu.Item(tr("重新载入视图")).Disabled(!q.configConflict).Chosen() && q.configConflict {
			delete(q.drafts, m.taskView)
			delete(m.taskViewDrafts, m.taskView)
			if m.currentSavedView() == nil && m.taskView != "" {
				m.selectTaskView("")
			} else {
				m.restoreView(m.taskViewBaseline())
			}
			q.configConflict, q.problem = false, ""
			m.invalidateTaskQuery()
		}

		if menu.Item(tr("取消转换")).Disabled(!converting).Chosen() && converting {
			old := m.taskView
			delete(q.conversion, old)
			delete(q.drafts, old)
			m.selectTaskView("")
			delete(m.taskViewDrafts, old)
			delete(q.browsing, old)
		}
		if menu.Item(tr("还原草稿")).Disabled(converting || (!m.taskViewChanged() && !m.customQueryChanged())).Chosen() {
			delete(q.drafts, m.taskView)
			delete(m.taskViewDrafts, m.taskView)
			m.restoreView(m.taskViewBaseline())
			q.page, q.slice = 0, nil
			m.invalidateTaskQuery()
		}
		if menu.Item(tr("另存为新视图…")).Disabled(active != nil || !m.taskViewChanged()).Chosen() && active == nil {
			m.openSaveTaskView()
		}
		if menu.Item(tr("另存为 CEL 视图")).Disabled(m.activeTaskQuery() != nil).Chosen() {
			m.queryTabAction("convert")
		}
		if len(m.savedTaskViews) != 0 {
			menu.Separator()
			for _, view := range m.savedTaskViews {
				if menu.Item(view.Name).Disabled(m.taskView == view.ID).Chosen() {
					m.selectTaskView(view.ID)
				}
			}
		}
	})
}

func (m *mockup) queryTabDialog(c *ui.Context) {
	q := &m.customQuery
	open := q.dialog != ""
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(370).Gap(12).Children(func() {
			label := map[string]string{"new": "新视图", "rename": "重命名", "copy": "复制视图", "delete": "删除视图", "convert": "另存为 CEL 视图"}[q.dialog]
			components.Title(c, tr(label))
			submitted := false
			if q.dialog == "delete" {
				components.Body(c, q.name)
			} else {
				if q.dialog == "convert" && m.search != "" {
					components.Muted(c, m.search)
				}
				submitted = ui.TextInput(c.Key("name"), &q.name).Label(tr("视图名称")).AutoFocus().Submitted()
			}
			ui.Row(c.Key("actions")).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c.Key("apply"), tr(map[bool]string{true: "删除", false: "保存"}[q.dialog == "delete"])).Disabled(strings.TrimSpace(q.name) == "").Clicked() || submitted {
					if m.applyQueryTabAction() {
						open = false
					}
				}
			})
			if q.problem != "" {
				components.Muted(c.Key("problem"), q.problem)
			}
		})
	})
	if !open {
		q.dialog = ""
	}
}

func (m *mockup) applyQueryTabAction() bool {
	q := &m.customQuery
	action, selected := q.dialog, m.taskView
	saved := m.currentSavedView()
	candidate := m.newTaskQueryView(q.name)
	var next string
	switch action {
	case "convert":
		candidate.State = m.viewState()
		original := candidate.State.Query
		candidate.State.Query, candidate.State.Page = "", 0
		applyLegacyQueryDisplay(candidate.Query, candidate.State)
		name := strings.TrimSpace(candidate.Name)
		if name == "" || slices.ContainsFunc(m.savedTaskViews, func(view savedTaskView) bool { return strings.EqualFold(view.Name, name) }) {
			q.problem = tr("视图名称已存在")
			return false
		}
		ids := []string{}
		for _, index := range m.taskRows() {
			ids = append(ids, m.objects[index].ID)
		}
		if q.conversion == nil {
			q.conversion = make(map[string]taskQueryConversion)
		}
		q.conversion[candidate.ID] = taskQueryConversion{candidate, original, q.liveRevision, ids}
		m.taskViewDrafts[candidate.ID] = candidate.State
		m.selectTaskView(candidate.ID)
		m.changeTaskQuery(*candidate.Query)
		return true
	case "copy":
		if saved == nil {
			return false
		}
		candidate.State = m.viewState()
		candidate.Query = saved.Query
		if view := m.activeTaskQuery(); view != nil && q.result != nil {
			view.Dependencies = slices.Clone(q.result.FieldDependencies)
			candidate.Query = view
		}
	case "rename":
		if saved == nil {
			return false
		}
		candidate = *saved
		candidate.Name = q.name
	}
	err := m.updateTaskQueryConfig(func(cfg *taskQueryConfig) error {
		if action != "delete" {
			return putTaskQueryView(cfg, candidate)
		}
		index := slices.IndexFunc(cfg.Views, func(view savedTaskView) bool { return view.ID == selected })
		if index >= 0 {
			cfg.Views = slices.Delete(cfg.Views, index, index+1)
			if len(cfg.Views) > 0 {
				next = cfg.Views[min(index, len(cfg.Views)-1)].ID
			}
		}
		return nil
	})
	m.setTaskQueryProblem(err)
	if err != nil {
		return false
	}
	if action == "delete" {
		m.selectTaskView(next)
		delete(q.browsing, selected)
		delete(q.drafts, selected)
		delete(q.effective, selected)
		delete(m.taskViewDrafts, selected)
	} else if action != "rename" {
		m.selectTaskView(candidate.ID)
	}
	return true
}

func (m *mockup) customQueryControls(c *ui.Context, view taskQueryView) {
	q := &m.customQuery
	m.ensureCustomQuery(view)
	wasCondition, wasExpression, wasFields := q.conditionOpen, q.expressionOpen, q.fieldsOpen
	ui.Row(c.Key("query")).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		input := ui.SearchField(c.Key("keyword"), &view.Keyword).Label(tr("搜索任务")).Placeholder(tr("搜索任务")).MinWidth(60).Grow(1)
		if m.keyboard.focusSearch {
			input.Focus()
			m.keyboard.focusSearch = false
		}
		m.keyboard.editingText = input.Focused()
		if input.Changed() {
			m.changeTaskQuery(view)
		}
		if input.Submitted() {
			q.loaded = ""
			m.scheduleCustomQuery(view, 0)
		}
		condition := components.IconButton(c.Key("condition"), "line.3.horizontal.decrease.circle", tr("添加条件")+" · F", false)
		if q.returnFocus == "condition" {
			condition.Focus()
			q.returnFocus = ""
		}
		if condition.Clicked() || (!input.Focused() && !m.fullTask && !q.expressionOpen && !q.fieldsOpen && !q.viewOpen && c.Shortcut(0, ui.KeyF)) {
			q.conditionOpen = true
		}
		expression := components.IconToggle(c.Key("expression"), "chevron.left.forwardslash.chevron.right", tr("表达式"), q.expressionOpen)
		if q.returnFocus == "expression" {
			expression.Focus()
			q.returnFocus = ""
		}
		if expression.Clicked() {
			q.expressionOpen = !q.expressionOpen
		}
		settings := components.IconToggle(c.Key("view"), "slider.horizontal.3", tr("视图设置"), q.viewOpen)
		if q.returnFocus == "settings" {
			settings.Focus()
			q.returnFocus = ""
		}
		if settings.Clicked() {
			q.viewOpen = !q.viewOpen
		}
		m.taskControlsPopover(c.Key("view-popover"), settings, &q.viewOpen, func() { m.customQueryDisplay(c.Key("settings"), view) })
		if components.IconButton(c.Key("reset"), "xmark.circle", tr("清除筛选"), false).Clicked() {
			view.Keyword = ""
			view.editExpression("")
			q.slice = nil
			m.sourceFilter, m.sourceFilterID, m.statusFilter, m.taskWatchOnly = "全部来源", "", "全部状态", false
			m.changeTaskQuery(view)
		}
	})
	summary := func(c *ui.Context) {
		ui.Row(c).FillWidth().Wrap().Gap(6).AlignItems(ui.Center).Children(func() {
			if view.Expression != "" && (len(view.Chips) == 0 || view.ChipBase != "") {
				if ui.Button(c.Key("expression-summary"), tr("自定义表达式")).Clicked() {
					q.expressionOpen = true
				}
				if components.IconButton(c.Key("clear-expression"), "xmark", tr("清除表达式"), false).Clicked() {
					view.editExpression("")
					m.changeTaskQuery(view)
				}
			}
			for i, chip := range view.Chips {
				if ui.Button(c.Key(fmt.Sprintf("chip-%d", i)), chip.Label+" ×").Tooltip(tr("移除条件") + " · " + chip.Label).Clicked() {
					view.removeCondition(i)
					m.changeTaskQuery(view)
					break
				}
			}
			if m.sourceFilter != "全部来源" && ui.Button(c.Key("source"), tr(m.sourceFilter)+" ×").Clicked() {
				m.sourceFilter, m.sourceFilterID = "全部来源", ""
				q.page = 0
			}
			if m.statusFilter != "全部状态" && ui.Button(c.Key("status"), tr(m.statusFilter)+" ×").Clicked() {
				m.statusFilter = "全部状态"
				q.page = 0
			}
			if m.taskWatchOnly && ui.Button(c.Key("watch"), tr("关注中")+" ×").Clicked() {
				m.taskWatchOnly = false
				q.page = 0
			}
			if q.result != nil {
				components.Muted(c.Key("counts"), fmt.Sprintf(tr("%d / %d 项"), q.result.SliceTotal, q.result.QueryTotal))
			}
			if q.loading || !m.liveLoaded || m.liveStale {
				components.Muted(c.Key("loading"), tr("正在读取任务…"))
			}
		})
	}
	if m.compactTaskList() && len(view.Chips) > 1 {
		ui.Scroll(c.Key("summary-scroll")).FillWidth().Height(64).Children(func() {
			summary(c.Key("summary"))
		})
	} else {
		summary(c.Key("summary"))
	}
	if conversion, converting := q.conversion[m.taskView]; converting {
		if conversion.original != "" {
			components.Muted(c.Key("legacy-query"), conversion.original)
		}
		if q.comparison != "" {
			components.Muted(c.Key("comparison"), q.comparison)
		} else if q.result != nil && !q.loading {
			components.Muted(c.Key("comparison-stale"), tr("快照已更改，请重新开始转换"))
		}
	}
	if q.diagnostic != "" {
		components.StatusLabel(c.Key("diagnostic"), components.Status{Text: q.diagnostic, Icon: "exclamationmark.circle", Color: c.Theme().Warning})
	}
	if q.problem != "" {
		components.Muted(c.Key("config-problem"), q.problem)
	}
	ui.Modal(c.Key("editor"), &q.expressionOpen, func() {
		ui.Column(c).Width(440).Gap(8).Children(func() {
			components.Title(c, tr("CEL 表达式"))
			current := view.Expression
			if ui.TextArea(c.Key("text"), &current).Label(tr("CEL 表达式")).Height(110).FillWidth().Changed() {
				view.editExpression(current)
				m.changeTaskQuery(view)
				m.ensureCustomQuery(view)
			}
			if q.diagnostic != "" {
				components.Muted(c.Key("diagnostic"), q.diagnostic)
			}
			ui.Row(c.Key("actions")).Gap(8).Children(func() {
				if ui.Button(c.Key("copy"), tr("复制表达式")).Clicked() {
					c.WriteClipboard(view.Expression)
				}
				if ui.Button(c.Key("done"), tr("完成")).Clicked() {
					q.expressionOpen = false
				}
			})
		})
	})
	m.queryConditionDialog(c.Key("condition-dialog"), view)
	m.queryFieldsDialog(c.Key("fields-dialog"))
	switch {
	case wasFields && !q.fieldsOpen:
		q.returnFocus = "settings"
	case wasCondition && !q.conditionOpen:
		q.returnFocus = "condition"
	case wasExpression && !q.expressionOpen:
		q.returnFocus = "expression"
	}
	if q.returnFocus != "" {
		c.Invalidate()
	}
}

func queryFieldTitle(field common.WorkbenchQueryFieldInfo) string {
	names := map[string]string{"task.title": "任务", "task.id": "任务 ID", "task.status": "状态", "task.source_id": "来源",
		"task.labels": "标签", "task.assignees": "经办人", "task.due": "截止日", "task.priority": "优先级", "task.updated": "最近变化"}
	if name := names[field.ID]; name != "" {
		return tr(name)
	}
	return field.Name
}

func queryFieldSelect(c *ui.Context, id *string, fields []common.WorkbenchQueryFieldInfo, label, none string, allow func(common.WorkbenchQueryFieldInfo) bool) bool {
	ids, labels := []string{}, []string{}
	if none != "" {
		ids, labels = append(ids, ""), append(labels, none)
	}
	for _, field := range fields {
		if allow(field) {
			ids = append(ids, field.ID)
			name := queryFieldTitle(field)
			if slices.Contains(labels, name) {
				name += " · " + field.Key
			}
			labels = append(labels, name)
		}
	}
	if len(ids) == 0 {
		return false
	}
	selected := labels[0]
	if i := slices.Index(ids, *id); i >= 0 {
		selected = labels[i]
	}
	var changed bool
	ui.Field(c, label, func() { changed = ui.Select(c.Key("choose"), &selected, labels).Label(label).FillWidth().Changed() })
	if changed {
		if i := slices.Index(labels, selected); i >= 0 && ids[i] != *id {
			*id = ids[i]
			return true
		}
	}
	return false
}

func (m *mockup) queryConditionDialog(c *ui.Context, view taskQueryView) {
	q := &m.customQuery
	ui.Modal(c, &q.conditionOpen, func() {
		ui.Column(c).Width(390).Gap(8).Children(func() {
			components.Title(c, tr("添加条件"))
			if q.result == nil || len(q.result.Fields) == 0 {
				components.Muted(c, tr("正在读取任务…"))
				return
			}
			fields := q.result.Fields
			_ = queryFieldSelect(c.Key("field"), &q.conditionField, fields, tr("字段"), "", func(common.WorkbenchQueryFieldInfo) bool { return true })
			i := slices.IndexFunc(fields, func(field common.WorkbenchQueryFieldInfo) bool { return field.ID == q.conditionField })
			if i < 0 {
				i = 0
			}
			field := fields[i]
			operations := conditionOperations(field)
			if !slices.Contains(operations, q.conditionOp) {
				q.conditionOp = operations[0]
			}
			localizedSelect(c.Key("operator"), &q.conditionOp, operations, 0).Label(tr("运算符"))
			inputRequired := !slices.Contains([]string{"有值", "缺值", "本周", "未来七天", "逾期"}, q.conditionOp)
			submit := false
			if inputRequired {
				m.conditionSuggestions(c.Key("suggestions"), field)
				submit = components.TextField(c.Key("value"), tr("条件值"), &q.conditionValue).Submitted()
			}
			if ui.PrimaryButton(c.Key("apply"), tr("应用")).Clicked() || submit {
				raw := q.conditionValue
				if field.Type == "single_select" && !strings.HasPrefix(field.ID, "task.") {
					raw = foldQueryOption(raw)
				}
				condition, err := taskQueryCondition(field, q.conditionOp, raw)
				if err == nil {
					label := strings.TrimSpace(queryFieldTitle(field) + " " + tr(q.conditionOp) + " " + q.conditionValue)
					view.addCondition(label, condition)
					m.changeTaskQuery(view)
					q.conditionOpen, q.conditionValue = false, ""
				} else {
					q.problem = err.Error()
				}
			}
			if q.problem != "" {
				components.Muted(c, q.problem)
			}
		})
	})
}

func (m *mockup) conditionSuggestions(c *ui.Context, field common.WorkbenchQueryFieldInfo) {
	q := &m.customQuery
	options := []string{}
	switch field.ID {
	case "task.status":
		options = []string{"open", "in_progress", "done", "cancelled", "unknown"}
	case "task.source_id":
		for _, source := range q.result.Sources {
			options = append(options, source.ID)
		}
	default:
		for _, item := range q.result.Items {
			value := item.Fields[field.ID]
			if value.Key != "" && !slices.Contains(options, value.Key) {
				options = append(options, value.Key)
			}
			for _, key := range value.Keys {
				if !slices.Contains(options, key) {
					options = append(options, key)
				}
			}
		}
	}
	if len(options) > 0 {
		options = append([]string{tr("选择值")}, options...)
		selected := options[0]
		if ui.Select(c, &selected, options).Label(tr("选择值")).FillWidth().Changed() && selected != options[0] {
			q.conditionValue = selected
		}
	}
}

func (m *mockup) customQueryDisplay(c *ui.Context, view taskQueryView) {
	q := &m.customQuery
	if q.result == nil {
		return
	}
	display := &view.List
	if m.taskBoard {
		display = &view.Board
	}
	fields := q.result.Fields
	canGroup := func(f common.WorkbenchQueryFieldInfo) bool { return f.Group }
	canSort := func(f common.WorkbenchQueryFieldInfo) bool { return f.Sort }
	canSum := func(f common.WorkbenchQueryFieldInfo) bool { return f.Sum }
	changed := false
	if m.taskBoard {
		changed = queryFieldSelect(c.Key("columns"), &display.ColumnBy, fields, tr("分列"), tr("状态"), func(f common.WorkbenchQueryFieldInfo) bool {
			return canGroup(f) && f.ID != display.SwimlaneBy && f.ID != "task.status"
		}) || changed
		changed = queryFieldSelect(c.Key("lanes"), &display.SwimlaneBy, fields, tr("泳道"), tr("无"), func(f common.WorkbenchQueryFieldInfo) bool {
			return canGroup(f) && f.ID != display.ColumnBy && (display.ColumnBy != "" || f.ID != "task.status")
		}) || changed
	} else {
		changed = queryFieldSelect(c.Key("group"), &display.GroupBy, fields, tr("分组"), tr("无"), canGroup) || changed
		pageSize := tr("自动")
		if m.taskPageSizeOverride != 0 {
			pageSize = strconv.Itoa(m.taskPageSizeOverride)
		}
		if ui.Select(c.Key("page-size"), &pageSize, []string{tr("自动"), "10", "20", "50", "100"}).Label(tr("每页任务数")).Changed() {
			m.taskPageSizeOverride, _ = strconv.Atoi(pageSize)
			q.page = 0
		}
	}
	changed = queryFieldSelect(c.Key("sort"), &display.SortBy, fields, tr("排序字段"), tr("任务"), func(f common.WorkbenchQueryFieldInfo) bool { return canSort(f) && f.ID != "task.title" }) || changed
	if ui.Button(c.Key("sort-direction"), tr(map[bool]string{true: "降序", false: "升序"}[display.Descending])).Clicked() {
		display.Descending = !display.Descending
		changed = true
	}
	changed = queryFieldSelect(c.Key("sum"), &display.SumBy, fields, tr("数字合计"), tr("无"), canSum) || changed
	if queryFieldSelect(c.Key("slice"), &display.SliceBy, fields, tr("切片"), tr("无"), canGroup) {
		q.slice = nil
		view.List.SliceBy, view.Board.SliceBy = display.SliceBy, display.SliceBy
		changed = true
	}
	ui.Divider(c)
	components.Heading(c, tr("显示字段"))
	for i, id := range slices.Clone(display.Fields) {
		fieldIndex := slices.IndexFunc(fields, func(f common.WorkbenchQueryFieldInfo) bool { return f.ID == id })
		name := id
		if fieldIndex >= 0 {
			name = queryFieldTitle(fields[fieldIndex])
		}
		ui.Row(c.Key(id)).Gap(4).Children(func() {
			components.Body(c, name)
			if !m.taskBoard {
				width := strconv.Itoa(int(m.queryListFieldWidth(view, id, 104)))
				input := ui.TextInput(c.Key("width"), &width).Label(tr("列宽") + " " + name).Width(54)
				if input.Submitted() {
					if pixels, err := strconv.Atoi(width); err == nil && pixels >= 72 && pixels <= 480 {
						view.Widths = maps.Clone(view.Widths)
						if view.Widths == nil {
							view.Widths = make(map[string]float32)
						}
						view.Widths[id] = float32(pixels)
						changed = true
					}
				}
			}
			ui.Spacer(c).Grow(1)
			if components.IconButton(c.Key("up"), "chevron.up", tr("上移")+" "+name, i == 0).Clicked() && i > 0 {
				display.Fields = slices.Clone(display.Fields)
				display.Fields[i], display.Fields[i-1] = display.Fields[i-1], display.Fields[i]
				changed = true
			}
			if components.IconButton(c.Key("remove"), "xmark", tr("移除")+" "+name, false).Clicked() {
				display.Fields = slices.Delete(slices.Clone(display.Fields), i, i+1)
				changed = true
			}
		})
		if changed && len(display.Fields) <= i {
			break
		}
	}
	added := ""
	if queryFieldSelect(c.Key("add"), &added, fields, tr("添加字段"), tr("添加字段"), func(f common.WorkbenchQueryFieldInfo) bool {
		return f.Display && f.ID != "task.title" && !slices.Contains(display.Fields, f.ID)
	}) && added != "" {
		display.Fields = append(slices.Clone(display.Fields), added)
		changed = true
	}
	if ui.Button(c.Key("manage"), tr("管理字段")).Clicked() {
		q.viewOpen, q.fieldsOpen = false, true
	}
	if changed {
		view.List.SortBy, view.Board.SortBy = display.SortBy, display.SortBy
		view.List.Descending, view.Board.Descending = display.Descending, display.Descending
		view.List.SumBy, view.Board.SumBy = display.SumBy, display.SumBy
		m.changeTaskQuery(view)
	}
}

func applyLegacyQueryDisplay(view *taskQueryView, state taskViewState) {
	ids := map[string]string{"title": "task.title", "status": "task.status", "priority": "task.priority", "source": "task.source_id",
		"due": "task.due", "updated": "task.updated", "labels": "task.labels", "assignee": "task.assignees",
		"file": "native.file", "path": "native.path", "section": "native.section"}
	order := state.TableLayout.Order
	if len(order) == 0 {
		order = []string{"title", "status", "priority", "source", "due", "updated", "labels", "assignee", "file", "path", "section"}
	}
	view.List.Fields = []string{}
	for _, field := range order {
		if id := ids[field]; id != "" && field != "title" && !state.Columns[field] {
			view.List.Fields = append(view.List.Fields, id)
		}
	}
	for field, width := range state.TableLayout.Widths {
		if id := ids[field]; id != "" {
			if view.Widths == nil {
				view.Widths = make(map[string]float32)
			}
			view.Widths[id] = width
		}
	}
	view.List.SortBy, view.Board.SortBy = ids[state.Sort.Column], ids[state.Sort.Column]
	view.List.Descending, view.Board.Descending = state.Sort.Descending, state.Sort.Descending
	switch state.Grouping {
	case "按状态分组":
		view.List.GroupBy = "task.status"
	case "按来源分组":
		view.List.GroupBy = "task.source_id"
	}
}
