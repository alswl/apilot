package workbench

import (
	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type taskStateMapping struct {
	label, native string
}

var obsidianStates = [...]taskStateMapping{
	{"待处理", " "}, {"进行中", "/"}, {"已完成", "x"}, {"不计划处理", "-"},
}

var githubStates = [...]taskStateMapping{
	{"开放", "open"}, {"已完成", "closed:completed"}, {"不计划处理", "closed:not_planned"},
}

func (conn *connection) stateMappings() []taskStateMapping {
	if conn.SourceKind == githubIssues {
		return githubStates[:]
	}
	states := obsidianStates
	for i, state := range states {
		if native, ok := conn.CheckboxMapping[state.label]; ok {
			states[i].native = native
		}
	}
	return states[:]
}

func (conn *connection) reopenState() string {
	if conn.SourceKind == githubIssues {
		return "开放"
	}
	return "待处理"
}

func (m *mockup) taskStateMenu(o sourceObject, menu *ui.Menu) {
	conn := m.taskConnection(o)
	if conn == nil {
		return
	}
	current := sourceState(o, conn.SourceKind)
	disabled := !m.canToggleSourceTask(o) || m.sourceWritePending(o.ID)
	for _, state := range conn.stateMappings() {
		if menu.Item(conn.displayState(state.label)).Radio(state.label == current).Disabled(disabled).Chosen() {
			m.setTaskState(o, state.label)
		}
	}
}

func (m *mockup) taskStatusCheckbox(c *ui.Context, o sourceObject) {
	confirmed := taskClosed(o)
	checked := confirmed
	label := tr(m.sourceToggleLabel(o))
	box := ui.CheckboxBase(c, &checked).Checked(confirmed).Label(label).Tooltip(label).
		Disabled(!m.canToggleSourceTask(o) || m.sourceWritePending(o.ID))
	box.ContextMenu(func(menu *ui.Menu) { m.taskStateMenu(o, menu) })
	if box.Changed() {
		m.toggleSourceTask(o)
		c.Invalidate()
	}
	box.Children(func() {
		icon, color := "square", c.Theme().TextMuted
		if confirmed {
			icon, color = "checkmark.square.fill", c.Theme().Accent
		}
		components.Symbol(c, icon, color)
	})
	if m.live != nil {
		if m.sourceWritePending(o.ID) {
			ui.Spinner(c.Key("pending")).Size(13, 13).Label(tr("等待来源确认")).Tooltip(tr("等待来源确认"))
		} else if problem := m.liveTaskProblems[o.ID]; problem != "" {
			ui.Box(c.Key("write-problem")).Label(tr(problem)).Tooltip(tr(problem)).Children(func() {
				components.Symbol(c, "exclamationmark.circle", c.Theme().Warning)
			})
		}
	}
}

func (m *mockup) openTaskButton(c *ui.Context) {
	if components.IconButton(c.Key("open"), "arrow.up.right.square", tr("打开完整任务"), false).
		Tooltip(tr("打开完整任务") + " · " + m.keyboard.display("task.open")).Clicked() {
		m.openCurrentTask()
	}
}

func sourceState(o sourceObject, kind string) string {
	if o.State != "" {
		return workbenchStateLabel(o.State)
	}
	if kind == githubIssues {
		if o.NativeFields["state"] != "closed" {
			return "开放"
		}
		if o.NativeFields["state_reason"] == "not_planned" {
			return "不计划处理"
		}
		return "已完成"
	}
	switch o.NativeFields["status"] {
	case "已完成", "进行中", "不计划处理":
		return o.NativeFields["status"]
	default:
		return "待处理"
	}
}

func (conn *connection) displayState(state string) string {
	if label := conn.StatusLabels[state]; label != "" {
		return label
	}
	return tr(state)
}

func (m *mockup) taskStateLabel(o sourceObject) string {
	if o.State != "" {
		return tr(workbenchStateLabel(o.State))
	}
	if conn := m.taskConnection(o); conn != nil {
		if label := conn.StatusLabels[sourceState(o, conn.SourceKind)]; label != "" {
			return label
		}
	}
	return tr(o.NativeFields["status"])
}
