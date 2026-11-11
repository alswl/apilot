package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/text/width"
)

const quickTaskLimit = 5

// Bound dynamic labels before building NSMenu: its widest item sets the width.
// This is a conservative text budget, not a pixel measurement of the system font.
func menuLabel(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	var out strings.Builder
	units := 0
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		n := 1
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n = 2
		}
		if unicode.Is(unicode.Mn, r) {
			n = 0
		}
		if units+n > 28 {
			out.WriteRune('…')
			break
		}
		out.WriteRune(r)
		units += n
	}
	return out.String()
}

func (s *windowState) openTasks() {
	s.tab, s.query = 0, ""
	s.selectedTask, s.selectedRun, s.selectedObserver = "", "", ""
	s.show()
}

func (s *windowState) openHistory(runID string) {
	s.tab = 1
	s.selectedTask, s.selectedObserver, s.selectedRun = "", "", runID
	s.show()
}

func (s *windowState) trayItems(status map[string]any) []*mygo.MenuItem {
	heading := "Apilot · 就绪"
	if count := status["running_count"]; count != nil && fmt.Sprint(count) != "0" {
		heading = fmt.Sprintf("Apilot · %v 项运行中", count)
	}
	if s.busy {
		heading = "Apilot · 正在处理…"
	}
	menu := []*mygo.MenuItem{{ID: "status", Label: heading, Disabled: true}}

	unread := 0
	attention := []*mygo.MenuItem{}
	for _, e := range s.events {
		if e.AcknowledgedAt != nil {
			continue
		}
		unread++
		if len(attention) < 3 {
			attention = append(attention, &mygo.MenuItem{ID: "event-" + e.ID, Label: menuLabel(e.Summary), ToolTip: e.Summary, Click: func(*mygo.MenuItem, *mygo.Window) { s.selectEvent(e); s.show() }})
		}
	}
	if unread > 0 {
		attention = append(attention, mygo.Separator(), &mygo.MenuItem{Label: "全部提醒…", Click: func(*mygo.MenuItem, *mygo.Window) { s.showAttention() }})
		menu = append(menu, &mygo.MenuItem{ID: "attention", Label: fmt.Sprintf("待关注（%d）", unread), Submenu: attention})
	}
	active := []*mygo.MenuItem{}
	activeCount := 0
	for _, r := range s.runs {
		if !r.Active() {
			continue
		}
		activeCount++
		if len(active) < 3 {
			active = append(active, &mygo.MenuItem{ID: "stop-" + r.ID, Label: menuLabel("停止「" + r.TaskName + "」"), ToolTip: "停止「" + r.TaskName + "」", Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) {
				s.action(func() error { _, e := s.service.StopRun(r.ID); return e })
			}})
		}
	}
	if activeCount > 0 {
		if activeCount > 3 {
			active = append(active, mygo.Separator(), &mygo.MenuItem{Label: "全部运行…", Click: func(*mygo.MenuItem, *mygo.Window) { s.openHistory("") }})
		}
		menu = append(menu, &mygo.MenuItem{ID: "active", Label: fmt.Sprintf("运行中（%d）", activeCount), Submenu: active})
	}
	if s.err != "" && !(s.busy && s.err == "正在退出…") {
		menu = append(menu, &mygo.MenuItem{ID: "last-error", Label: "操作失败 · 查看详情…", Click: func(*mygo.MenuItem, *mygo.Window) { s.show() }})
	}

	menu = append(menu, mygo.Separator(), &mygo.MenuItem{Label: "快捷任务", Disabled: true})
	for i, t := range s.tasks {
		if i >= quickTaskLimit {
			break
		}
		items := []*mygo.MenuItem{{Label: "立即运行", Disabled: s.busy || taskActive(s.runs, t.ID), Click: func(*mygo.MenuItem, *mygo.Window) {
			s.action(func() error { _, e := s.service.RunTask(t.ID, "manual"); return e })
		}}}
		if t.Schedule != nil {
			items = append(items, &mygo.MenuItem{Label: "自动运行", Type: mygo.MenuItemCheckbox, Checked: t.Enabled && t.Schedule.Enabled, Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) {
				s.action(func() error { return s.setAutomatic(t) })
			}})
		}
		items = append(items, &mygo.MenuItem{Label: "编辑…", Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) { s.editTask(&t) }})
		menu = append(menu, &mygo.MenuItem{ID: "task-" + t.ID, Label: menuLabel(t.Name), ToolTip: t.Name, Submenu: items})
	}
	if len(s.tasks) == 0 {
		menu = append(menu, &mygo.MenuItem{Label: "暂无任务", Disabled: true})
	}
	menu = append(menu,
		&mygo.MenuItem{ID: "new-task", Label: "新建任务…", Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) { s.editTask(nil) }},
		&mygo.MenuItem{ID: "manage", Label: "全部任务…", Click: func(*mygo.MenuItem, *mygo.Window) { s.openTasks() }}, mygo.Separator())

	recent := []*mygo.MenuItem{}
	for _, r := range s.runs {
		if r.Active() {
			continue
		}
		recent = append(recent, &mygo.MenuItem{ID: "run-" + r.ID, Label: menuLabel(statusText(r.Status) + " · " + r.TaskName), ToolTip: r.TaskName, Click: func(*mygo.MenuItem, *mygo.Window) { s.openHistory(r.ID) }})
		if len(recent) >= 3 {
			break
		}
	}
	if len(recent) > 0 {
		recent = append(recent, mygo.Separator())
	}
	recent = append(recent, &mygo.MenuItem{Label: "全部历史…", Click: func(*mygo.MenuItem, *mygo.Window) { s.openHistory("") }})
	menu = append(menu,
		&mygo.MenuItem{ID: "recent", Label: "执行历史", Submenu: recent},
		&mygo.MenuItem{ID: "settings", Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { s.showSettings() }},
		mygo.Separator(), &mygo.MenuItem{Label: "退出 Apilot", Role: mygo.RoleQuit})
	return menu
}

func (s *windowState) showAttention() {
	if s.attentionWin != nil && !s.attentionWin.IsDestroyed() {
		s.attentionWin.Show()
		return
	}
	s.attentionWin = mygo.NewWindow(mygo.WindowOptions{Title: "待关注", Width: 480, Height: 360, MinWidth: 360, MinHeight: 260, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).FillWidth().Padding(16).Gap(12).Children(func() {
				count := 0
				for _, e := range s.events {
					if e.AcknowledgedAt != nil {
						continue
					}
					count++
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						ui.Text(c, e.Summary).Wrap()
						if ui.Button(c, "查看来源").Key(e.ID).Clicked() {
							s.selectEvent(e)
							s.show()
						}
					})
				}
				if count == 0 {
					ui.Text(c, "暂无待关注事项")
				}
			})
		})
	})})
}
