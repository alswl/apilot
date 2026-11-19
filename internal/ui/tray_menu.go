package ui

import (
	"fmt"

	"github.com/alswl/apilot/internal/presentation"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func menuLabel(text string) string { return presentation.MenuLabel(text) }

func (s *windowState) openTasks() {
	if s.focusEditor() {
		return
	}
	s.tab, s.query = 0, ""
	s.selectedTask, s.selectedRun, s.selectedObserver = "", "", ""
	s.showAutomation()
}

func (s *windowState) openHistory(runID string) {
	if s.focusEditor() {
		return
	}
	s.tab = 1
	s.selectedTask, s.selectedObserver, s.selectedRun = "", "", runID
	s.showAutomation()
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
	menu := []*mygo.MenuItem{
		{ID: "workbench", Label: tr("openApilot"), Click: func(*mygo.MenuItem, *mygo.Window) { s.show() }},
		mygo.Separator(),
		{ID: "status", Label: heading, Disabled: true},
	}

	unread := projected.UnreadCount
	attention := []*mygo.MenuItem{}
	for _, e := range projected.Attention {
		attention = append(attention, &mygo.MenuItem{ID: "event-" + e.ID, Label: menuLabel(e.Summary), ToolTip: e.Summary, Click: func(*mygo.MenuItem, *mygo.Window) { s.selectEvent(e); s.showAutomation() }})
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
		if activeCount > len(projected.Active) {
			active = append(active, mygo.Separator(), &mygo.MenuItem{Label: tr("allRuns"), Click: func(*mygo.MenuItem, *mygo.Window) { s.openHistory("") }})
		}
		menu = append(menu, &mygo.MenuItem{ID: "active", Label: fmt.Sprintf(tr("runningD"), activeCount), Submenu: active})
	}
	if s.err != "" && !(s.busy && s.err == tr("quitting")) {
		menu = append(menu, &mygo.MenuItem{ID: "last-error", Label: tr("actionFailedViewDetails"), Click: func(*mygo.MenuItem, *mygo.Window) { s.showAutomation() }})
	}

	menu = append(menu, mygo.Separator())
	if len(projected.Tasks) > 0 {
		quick := make([]*mygo.MenuItem, 0, len(projected.Tasks))
		for _, task := range projected.Tasks {
			quick = append(quick, &mygo.MenuItem{
				ID: "task-" + task.ID, Label: menuLabel(task.Name), ToolTip: task.Name,
				Disabled: s.busy || taskActive(s.runs, task.ID),
				Click: func(*mygo.MenuItem, *mygo.Window) {
					s.action(func() error { _, err := s.service.RunTask(task.ID, "manual"); return err })
				},
			})
		}
		menu = append(menu, &mygo.MenuItem{ID: "quick-tasks", Label: tr("runNow"), Submenu: quick})
	}
	menu = append(menu,
		&mygo.MenuItem{ID: "new-task", Label: tr("newTask2"), Disabled: s.busy, Click: func(*mygo.MenuItem, *mygo.Window) { s.editTask(nil) }},
		&mygo.MenuItem{ID: "manage", Label: tr("allTasks"), Click: func(*mygo.MenuItem, *mygo.Window) { s.openTasks() }},
		mygo.Separator(),
		&mygo.MenuItem{ID: "settings", Label: tr("settings2"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { s.showSettings() }},
		mygo.Separator(), &mygo.MenuItem{ID: "quit", Label: tr("quitApilot"), Role: mygo.RoleQuit})
	return menu
}

func (s *windowState) showAttention() {
	if s.focusEditor() {
		return
	}
	s.tab = 3
	s.selectedTask, s.selectedRun, s.selectedObserver = "", "", ""
	s.showAutomation()
}

func (s *windowState) attentionList(c *ui.Context) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			count := 0
			for _, e := range s.events {
				if e.AcknowledgedAt != nil {
					continue
				}
				count++
				ui.Column(c.Key(e.ID)).FillWidth().Gap(4).Children(func() {
					ui.Text(c, e.Summary).Wrap()
					if ui.Button(c, tr("viewSource")).Clicked() {
						s.selectEvent(e)
					}
				})
			}
			if count == 0 {
				ui.Text(c, tr("noItemsNeedAttention"))
			}
		})
	})
}
