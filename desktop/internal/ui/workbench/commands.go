package workbench

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

var pageCommands = [...]string{"navigation.now", "navigation.tasks", "navigation.connections", "navigation.scheduled", "navigation.usage"}

type command struct {
	id, title string
	run       func()
}

func (m *mockup) navigate(page int) {
	if m.page == connectionsPage && page != connectionsPage {
		m.connectionReturnToTasks = false
	}
	if page == nowPage {
		m.nowLibraryOpen = false
	}
	if m.page != page {
		m.layout.detailExpanded, m.layout.detailReturnHidden = false, false
		m.taskBackStack = nil
		m.page, m.inspector, m.editingDue, m.fullTask = page, false, false, false
	}
	if page == tasksPage && !m.inspector {
		m.restoreTaskFocus = true
	}
}

func (m *mockup) openTaskConnections() {
	m.navigate(connectionsPage)
	m.connectionReturnToTasks = true
}

func (m *mockup) returnToTaskList() {
	m.navigate(tasksPage)
	filtered := m.search != "" || m.statusFilter != "全部状态" || m.sourceFilter != "全部来源" || m.taskWatchOnly
	_, problem := m.parseSearch()
	entryVisible := m.hasUnavailableTaskSource() || (len(m.taskRows()) == 0 && problem == "" && (m.liveError || (!filtered && m.liveLoaded)))
	m.restoreConnectionEntryFocus = entryVisible
	if !entryVisible {
		m.restoreTaskFocus = false
		m.keyboard.focusSearch = true
	}
}

func (m *mockup) focusTaskConnectionEntry(focus func()) {
	if m.restoreConnectionEntryFocus {
		focus()
		m.restoreConnectionEntryFocus = false
	}
}

func (m *mockup) closeInspector() {
	if m.taskBoard {
		m.layout.detailExpanded, m.layout.revealSelection = false, false
	}
	m.layout.detailReturnHidden = false
	m.taskBackStack = nil
	m.inspector, m.editingDue, m.fullTask = false, false, false
	m.restoreTaskFocus = m.page == tasksPage
}

func (m *mockup) showPalette(help bool) {
	m.keyboard.palette, m.keyboard.help = true, help
	m.keyboard.query, m.keyboard.cursor, m.keyboard.prefix = "", 0, keystroke{}
}

func (m *mockup) openSearch() {
	m.navigate(tasksPage)
	m.closeInspector()
	m.restoreTaskFocus, m.keyboard.focusSearch = false, true
}

func (m *mockup) moveSelection(delta int) {
	rows := m.taskNavigationRows()
	if len(rows) == 0 {
		return
	}
	if !m.taskBoard {
		m.syncTaskPage(len(rows))
	}
	current := slices.IndexFunc(rows, func(index int) bool { return m.objects[index].ID == m.taskSelection })
	if current < 0 {
		first, last := 0, len(rows)
		if !m.taskBoard {
			first = m.taskPageIndex * m.taskPageSize
			last = min(first+m.taskPageSize, len(rows))
		}
		current = first - 1
		if delta < 0 {
			current = last
		}
	}
	m.selectTask(rows[max(0, min(current+delta, len(rows)-1))])
}

func (m *mockup) selectTask(index int) {
	m.taskBackStack = nil
	if m.taskBoard {
		m.layout.revealSelection = true
	} else {
		m.locateTaskPage(m.taskRows(), index)
	}
	m.taskSelection = m.objects[index].ID
	if m.inspector {
		m.openTask(index)
	}
	m.restoreTaskFocus = true
}

func (m *mockup) activeCommands() []command {
	items := []command{
		{"command.palette", tr("打开命令面板"), func() { m.showPalette(false) }},
		{pageCommands[nowPage], tr("前往 Now"), func() { m.navigate(nowPage) }},
		{pageCommands[tasksPage], tr("前往任务"), func() { m.navigate(tasksPage) }},
		{pageCommands[connectionsPage], tr("前往连接"), func() { m.navigate(connectionsPage) }},
		{pageCommands[scheduledPage], tr("前往定时任务"), func() { m.navigate(scheduledPage) }},
		{pageCommands[usagePage], tr("前往用量"), func() { m.navigate(usagePage) }},
		{"shortcuts.help", tr("查看快捷键"), func() { m.showPalette(true) }},
		{"settings.open", tr("打开设置"), m.openSettings},
		{"tasks.search", tr("搜索任务"), m.openSearch},
	}
	if len(m.writableConnections()) > 0 {
		items = append(items, command{"task.new", tr("新建任务"), m.openNewTask})
	}
	if m.live == nil && len(m.objects) > 0 && !m.inspector && (m.page == nowPage || (m.unifiedTasks() && m.taskSelection == "")) {
		if m.objects[0].NativeFields["status"] == "已完成" {
			items = append(items, command{"now.reply", tr("查看客户答复任务"), func() { m.openNowTask(1) }})
		} else if m.variant != 1 && !m.briefDismissed {
			items = append(items, command{"task.evidence", tr("查看退款验收依据"), func() { m.openNowTask(0) }})
			if !m.watching {
				items = append(items, command{"task.watch", tr("关注退款验收"), func() { m.setWatching(true) }})
			}
		}
	}
	if m.page == tasksPage {
		rows := m.taskNavigationRows()
		if m.taskSelection != "" || len(rows) > 0 || m.inspector {
			items = append(items, command{"task.open", tr("打开完整任务"), m.openCurrentTask})
		}
		items = append(items, command{"tasks.insert", tr("输入任务搜索"), m.openSearch}, command{"view.save", tr("保存视图"), m.saveCurrentTaskView})
		if !m.taskBoard && (!m.inspector || m.unifiedTasks()) && m.taskPageIndex > 0 {
			items = append(items, command{"view.prevPage", tr("上一页"), func() { m.browseTaskPage(-1) }})
		}
		if !m.taskBoard && (!m.inspector || m.unifiedTasks()) && m.taskPageIndex+1 < m.taskPages(len(rows)) {
			items = append(items, command{"view.nextPage", tr("下一页"), func() { m.browseTaskPage(1) }})
		}
		if len(rows) > 0 {
			items = append(items,
				command{"tasks.first", tr("首项任务"), func() { m.selectTask(rows[0]) }},
				command{"tasks.last", tr("末项任务"), func() { m.selectTask(rows[len(rows)-1]) }},
				command{"tasks.next", tr("下一项任务"), func() { m.moveSelection(1) }},
				command{"tasks.previous", tr("上一项任务"), func() { m.moveSelection(-1) }},
			)
		}
	}
	index := -1
	if m.inspector {
		index = m.selected
	} else if m.page == tasksPage && m.taskSelection != "" {
		index = slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == m.taskSelection })
	}
	if index < 0 || (m.page == tasksPage && m.taskBoard && !m.inspector) {
		return items
	}
	object := m.objects[index]
	if m.editableSource(object) && !m.sourceWritePending(object.ID) {
		items = append(items,
			command{"task.edit", tr("编辑任务"), func() { m.openSourceEdit(object) }},
			command{"task.labels", tr("管理标签"), func() { m.openLabels(object) }},
		)
	}

	if m.canToggleSourceTask(object) && !m.sourceWritePending(object.ID) {
		items = append(items, command{"task.toggle", tr(m.sourceToggleLabel(object)), func() { m.toggleSourceTask(object) }})
	}

	if m.objects[index].NativeFields["excerpt"] != "" {
		items = append(items, command{"task.evidence", tr("查看当前任务依据"), func() { m.openTask(index) }})
	}
	if m.objects[index].ID == m.mutation.SourceObject {
		label := tr("关注退款验收")
		if m.watching {
			label = tr("取消关注退款验收")
		}
		items = append(items, command{"task.watch", label, func() { m.setWatching(!m.watching) }})
		if m.mutation.Status == "confirmed" {
			items = append(items, command{"task.editDeadline", tr("编辑当前任务截止日"), func() {
				m.openTask(index)
				m.beginDeadlineEdit(&m.objects[index])
			}})
		}
	} else {
		label := tr("关注任务")
		if m.isWatched(m.objects[index].ID) {
			label = tr("取消关注任务")
		}
		items = append(items, command{"task.watch", label, func() {
			id := m.objects[index].ID
			m.setTaskWatched(id, !m.isWatched(id))
		}})
	}
	return items
}

func (m *mockup) dispatchKeyboard(c *ui.Context, commands []command) {
	k := &m.keyboard
	if m.connectorLogin != nil || m.connectorMapping != nil || m.connectorRuns != nil || m.connectorTaskEditor != nil || k.palette || m.assistantOpen || m.connectorEditor != nil || m.syncLogID != "" || m.newTaskOpen || m.debugOpen || m.saveTaskViewOpen || m.taskResourceOpen || m.sourceEditor != nil || m.labelEditor != nil ||
		(m.compactTaskList() && (m.taskDisplayOpen || m.searchExpanded)) || (m.taskBoard && m.searchExpanded) {
		k.prefix = keystroke{}
		return
	}
	if m.page == connectionsPage && m.connectionReturnToTasks && c.Shortcut(0, ui.KeyEscape) {
		m.returnToTaskList()
		c.Invalidate()
		return
	}
	if k.editingText || time.Now().After(k.expires) {
		k.prefix = keystroke{}
	}
	if k.prefix.key != 0 {
		held := c.KeysDown()
		if len(held) > 0 {
			recent := keystroke{key: held[len(held)-1], mods: c.Modifiers()}
			if recent != k.prefix && !slices.ContainsFunc(k.bindings, func(item binding) bool {
				return item.first == k.prefix && item.second == recent
			}) {
				k.prefix = keystroke{}
			}
		}
	}
	if (m.inspector || m.layout.detailExpanded || k.editingText) && c.Shortcut(0, ui.KeyEscape) {
		if k.editingText {
			m.restoreTaskFocus = m.page == tasksPage && len(m.taskRows()) > 0
			k.focusNavigation = !m.restoreTaskFocus
		} else if m.editingDue {
			m.editingDue = false
		} else if len(m.taskBackStack) > 0 {
			m.returnRelatedPending = true
		} else if m.taskBoard && m.layout.detailExpanded {
			m.leaveTaskDetail()
		} else if m.layout.detailExpanded {
			m.layout.detailExpanded = false
			m.layout.focusDetail = true
		} else {
			m.leaveTaskDetail()
		}
		c.Invalidate()
		return
	}
	if m.page == tasksPage && !m.inspector && !k.editingText && m.taskSelection != "" && c.Shortcut(0, ui.KeyEnter) {
		index := slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == m.taskSelection })
		if index >= 0 {
			m.openFullTask(index)
			c.Invalidate()
		}
		return
	}
	for _, item := range k.bindings {
		if item.plain && (k.editingText || m.editingDue || (m.page == scheduledPage && m.hostPanels.Automation != nil)) {
			continue
		}
		index := slices.IndexFunc(commands, func(cmd command) bool { return item.command == cmd.id })
		if index < 0 {
			continue
		}
		if k.prefix.key != 0 {
			if item.first != k.prefix || item.second.key == 0 || !c.Shortcut(item.second.mods, item.second.key) {
				continue
			}
		} else {
			if !c.Shortcut(item.first.mods, item.first.key) {
				continue
			}
			if item.second.key != 0 {
				if !c.ShortcutRepeated() {
					k.prefix, k.expires = item.first, time.Now().Add(750*time.Millisecond)
				}
				return
			}
		}
		k.prefix = keystroke{}
		if !c.ShortcutRepeated() || item.command == "tasks.next" || item.command == "tasks.previous" {
			commands[index].run()
			c.Invalidate()
		}
		return
	}
}

type commandResult struct {
	title, metadata string
	run             func()
}

func (m *mockup) paletteResults(commands []command) []commandResult {
	var result []commandResult
	query := strings.Fields(strings.ToLower(m.keyboard.query))
	matches := func(text string) bool {
		for _, word := range query {
			if !strings.Contains(strings.ToLower(text), word) {
				return false
			}
		}
		return true
	}
	for _, cmd := range commands {
		if cmd.id != "command.palette" && matches(cmd.title+" "+cmd.id) {
			result = append(result, commandResult{cmd.title, m.keyboard.display(cmd.id), cmd.run})
		}
	}
	for i, obj := range m.objects {
		if matches(m.sourceText(obj.Title) + " " + displayTaskID(obj) + " " + m.sourceText(obj.Locator)) {
			result = append(result, commandResult{m.sourceText(obj.Title), displayTaskID(obj) + " · " + tr(m.connectionName(obj.ConnectionID)), func() {
				m.navigate(tasksPage)
				m.openFullTask(i)
			}})
		}
	}
	return result
}

func (m *mockup) commandPalette(c *ui.Context, commands []command) {
	k := &m.keyboard
	ui.Modal(c, &k.palette, func() {
		ui.Column(c).Width(500).MaxHeight(460).Gap(12).Children(func() {
			if k.help {
				components.Title(c, tr("快捷键"))
				ui.Scroll(c.Key("bindings")).MaxHeight(320).FillWidth().Children(func() {
					ui.Column(c).FillWidth().Gap(8).Children(func() {
						components.Muted(c, tr("列表 ↑↓ 移动 · Return 打开 · Esc 返回"))
						for _, id := range slices.Sorted(maps.Keys(defaultBindings)) {
							if keys := k.display(id); keys != "" {
								label := tr(shortcutTitle(id))
								ui.Row(c.Key(id)).FillWidth().Gap(12).Children(func() {
									components.DataText(c, label).Grow(1)
									components.Muted(c, keys)
								})
							}
						}
					})
				})
				return
			}
			search := ui.SearchField(c.Key("query"), &k.query).Label(tr("搜索命令或任务")).Placeholder(tr("搜索命令或任务")).AutoFocus()
			if search.Changed() {
				k.cursor = 0
			}
			results := m.paletteResults(commands)
			if len(results) == 0 {
				components.Muted(c, tr("没有匹配的命令或任务"))
				return
			}
			delta := 0
			if c.Shortcut(0, ui.KeyDown) {
				delta = 1
			} else if c.Shortcut(0, ui.KeyUp) {
				delta = -1
			}
			k.cursor = max(0, min(k.cursor+delta, len(results)-1))
			execute := func(index int) {
				k.palette = false
				results[index].run()
				c.Invalidate()
			}
			if search.Submitted() || c.Shortcut(0, ui.KeyEnter) {
				execute(k.cursor)
				return
			}
			first := max(0, k.cursor-6)
			for i := first; i < min(first+7, len(results)); i++ {
				item := results[i]
				if components.ListRow(c.Key(i), components.ListItem{
					Title: item.title, Subtitle: item.metadata, Selected: i == k.cursor,
				}) {
					execute(i)
				}
			}
		})
	})
}

func shortcutTitle(id string) string {
	return map[string]string{
		"command.palette": "打开命令面板", "navigation.now": "前往 Now", "navigation.tasks": "前往任务",
		"navigation.connections": "前往连接", "navigation.scheduled": "前往定时任务", "navigation.usage": "前往用量",
		"tasks.next": "下一项任务（任务表）", "tasks.previous": "上一项任务（任务表）",
		"tasks.first": "首项任务（任务表）", "tasks.last": "末项任务（任务表）", "tasks.insert": "输入任务搜索",
		"tasks.search": "搜索任务", "shortcuts.help": "查看快捷键",
		"task.evidence": "查看当前任务依据", "task.watch": "切换退款验收关注",
		"task.editDeadline": "编辑当前任务截止日", "settings.open": "打开设置",
		"task.new": "新建任务", "now.reply": "查看客户答复任务", "view.save": "保存视图", "task.open": "打开完整任务", "task.edit": "编辑任务", "task.labels": "管理标签", "view.prevPage": "上一页", "view.nextPage": "下一页", "task.toggle": "切换任务状态",
	}[id]
}
