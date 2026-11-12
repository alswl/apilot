package ui

import (
	"fmt"

	"github.com/alswl/apilot/internal/presentation"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const quickTaskLimit = presentation.QuickTaskLimit

func menuLabel(text string) string { return presentation.MenuLabel(text) }

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
	projected := presentation.ProjectMenu(s.tasks, s.runs, s.events)
	heading := tr("apilotReady")
	if count := status["running_count"]; count != nil && fmt.Sprint(count) != "0" {
		heading = fmt.Sprintf(tr("apilotVRunning"), count)
	}
	if s.busy {
		heading = tr("apilotWorking")
	}
	menu := []*mygo.MenuItem{{ID: "status", Label: heading, Disabled: true}}

	unread := projected.UnreadCount
	attention := []*mygo.MenuItem{}
	for _, e := range projected.Attention {
		attention = append(attention, &mygo.MenuItem{ID: "event-" + e.ID, Label: menuLabel(e.Summary), ToolTip: e.Summary, Click: func(*mygo.MenuItem, *mygo.Window) { s.selectEvent(e); s.show() }})
	}
	if unread > 0 {
		attention = append(attention, mygo.Separator(), &mygo.MenuItem{Label: tr("allAlerts"), Click: func(*mygo.MenuItem, *mygo.Window) { s.showAttention() }})
		menu = append(menu, &mygo.MenuItem{ID: "attention", Label: fmt.Sprintf(tr("attentionD"), unread), Submenu: attention})
	}
	active := []*mygo.MenuItem{}
	activeCount := projected.ActiveCount
	for _, r := range projected.Active {
		active = append(active, &mygo.MenuItem{ID: "stop-" + r.ID, Label: menuLabel(fmt.Sprintf(tr("stopS"), r.TaskName)), ToolTip: fmt.Sprintf(tr("stopS"), r.TaskName), Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) {
			s.action(func() error { _, e := s.service.StopRun(r.ID); return e })
		}})
	}
	if activeCount > 0 {
		if activeCount > 3 {
			active = append(active, mygo.Separator(), &mygo.MenuItem{Label: tr("allRuns"), Click: func(*mygo.MenuItem, *mygo.Window) { s.openHistory("") }})
		}
		menu = append(menu, &mygo.MenuItem{ID: "active", Label: fmt.Sprintf(tr("runningD"), activeCount), Submenu: active})
	}
	if s.err != "" && !(s.busy && s.err == tr("quitting")) {
		menu = append(menu, &mygo.MenuItem{ID: "last-error", Label: tr("actionFailedViewDetails"), Click: func(*mygo.MenuItem, *mygo.Window) { s.show() }})
	}

	menu = append(menu, mygo.Separator(), &mygo.MenuItem{Label: tr("quickTasks"), Disabled: true})
	for _, t := range projected.Tasks {
		items := []*mygo.MenuItem{{Label: tr("runNow"), Disabled: s.busy || taskActive(s.runs, t.ID), Click: func(*mygo.MenuItem, *mygo.Window) {
			s.action(func() error { _, e := s.service.RunTask(t.ID, "manual"); return e })
		}}}
		if t.Schedule != nil {
			items = append(items, &mygo.MenuItem{Label: tr("automaticRuns"), Type: mygo.MenuItemCheckbox, Checked: t.Enabled && t.Schedule.Enabled, Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) {
				s.action(func() error { return s.setAutomatic(t) })
			}})
		}
		items = append(items, &mygo.MenuItem{Label: tr("edit2"), Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) { s.editTask(&t) }})
		menu = append(menu, &mygo.MenuItem{ID: "task-" + t.ID, Label: menuLabel(t.Name), ToolTip: t.Name, Submenu: items})
	}
	if len(s.tasks) == 0 {
		menu = append(menu, &mygo.MenuItem{Label: tr("noTasks"), Disabled: true})
	}
	menu = append(menu,
		&mygo.MenuItem{ID: "new-task", Label: tr("newTask2"), Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) { s.editTask(nil) }},
		&mygo.MenuItem{ID: "manage", Label: tr("allTasks"), Click: func(*mygo.MenuItem, *mygo.Window) { s.openTasks() }}, mygo.Separator())

	recent := []*mygo.MenuItem{}
	for _, r := range projected.Recent {
		recent = append(recent, &mygo.MenuItem{ID: "run-" + r.ID, Label: menuLabel(statusText(r.Status) + " · " + r.TaskName), ToolTip: r.TaskName, Click: func(*mygo.MenuItem, *mygo.Window) { s.openHistory(r.ID) }})
	}
	if len(recent) > 0 {
		recent = append(recent, mygo.Separator())
	}
	recent = append(recent, &mygo.MenuItem{Label: tr("allHistory"), Click: func(*mygo.MenuItem, *mygo.Window) { s.openHistory("") }})
	menu = append(menu,
		&mygo.MenuItem{ID: "recent", Label: tr("runHistory"), Submenu: recent},
		&mygo.MenuItem{ID: "settings", Label: tr("settings2"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { s.showSettings() }},
		mygo.Separator(), &mygo.MenuItem{Label: tr("quitApilot"), Role: mygo.RoleQuit})
	return menu
}

func (s *windowState) showAttention() {
	if s.attentionWin != nil && !s.attentionWin.IsDestroyed() {
		s.attentionWin.Show()
		return
	}
	s.attentionWin = mygo.NewWindow(mygo.WindowOptions{Title: tr("attention"), Width: 480, Height: 360, MinWidth: 360, MinHeight: 260, Content: ui.View(func(c *ui.Context) {
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
						if ui.Button(c, tr("viewSource")).Key(e.ID).Clicked() {
							s.selectEvent(e)
							s.show()
						}
					})
				}
				if count == 0 {
					ui.Text(c, tr("noItemsNeedAttention"))
				}
			})
		})
	})})
}
