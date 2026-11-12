package ui

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alswl/apilot/internal/presentation"
	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/server"
	"github.com/alswl/apilot/pkg/services"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type windowState struct {
	traySignature    string
	stopping         bool
	workers          sync.WaitGroup
	operation        presentation.OperationState
	refreshState     presentation.RefreshState
	logState         presentation.LogSelection
	logOperation     presentation.OperationState
	seen             map[string]bool
	ctx              context.Context
	taskScroll       ui.ScrollState
	runScroll        ui.ScrollState
	logScroll        ui.ScrollState
	followLogs       bool
	observers        []common.Observer
	selectedObserver string
	service          *services.Service
	win              *mygo.Window
	settingsWin      *mygo.Window
	cliWin           *mygo.Window
	attentionWin     *mygo.Window
	tray             *mygo.Tray
	tab              int
	tasks            []common.Task
	runs             []common.Run
	events           []common.AttentionEvent
	selectedTask     string
	selectedRun      string
	query            string
	stream           int
	logText          string
	logOffset        int64
	logKey           string
	err              string
	busy             bool
}

func Run(runtime *server.Runtime, probe bool) error {
	mygo.App.SetLanguages("en", "zh-Hans")
	service := runtime.Service
	var err error
	s := &windowState{service: service, followLogs: true, seen: presentation.InitialSeen(service.Runs(""), mustEvents(service))}
	appUI := mygo.App
	appUI.SetName("Apilot")
	appUI.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.ctx = ctx
	var allowQuit bool
	appUI.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if allowQuit {
			return
		}
		if e.SessionEnding {
			cancel()
			s.stopping = true
			go runtime.StopAdmission()
			return
		}
		e.PreventDefault()
		if s.stopping {
			return
		}
		s.stopping = true
		s.operation.Invalidate()
		s.refreshState.Invalidate()
		s.logOperation.Invalidate()
		s.err = tr("quitting")
		s.busy = true
		cancel()
		go func() {
			runtime.StopAdmission()
			runtime.StopIPC()
			s.workers.Wait()
			_ = runtime.Close()
			mygo.RunOnMain(func() { allowQuit = true; appUI.Quit() })
		}()
	})
	appUI.OnActivate(func(visible bool) {
		if !visible && s.tray != nil {
			s.show()
		}
	})
	appUI.OnWindowAllClosed(func() { log.Print("window closed; app remains running") })
	appUI.OnQuit(func() { cancel(); log.Print("app quit") })
	mygo.Power.OnSuspend(func() { service.Suspend() })
	mygo.Power.OnResume(func() { service.Resume() })
	appUI.WhenReady(func() {
		displayLocale.Store(appUI.Locale())
		appUI.OnLocaleChanged(func(locale string) {
			displayLocale.Store(locale)
			s.setAppMenu()
			refreshTitle := func(win *mygo.Window, title string) {
				if win != nil && !win.IsDestroyed() {
					win.SetTitle(title)
					win.Invalidate()
				}
			}
			refreshTitle(s.settingsWin, tr("settings"))
			refreshTitle(s.cliWin, tr("apilotCommandLineTool"))
			refreshTitle(s.attentionWin, tr("attention"))
			s.traySignature = ""
			s.refresh()
		})
		s.setAppMenu()
		s.tray, err = mygo.NewTray(mygo.TrayOptions{ID: "apilot", Icon: menuBarIcon, IconIsTemplate: true, ToolTip: "Apilot"})
		if err != nil {
			allowQuit = true
			appUI.Quit()
			return
		}
		log.Print("tray created")
		if !probe {
			s.offerCLI()
		}
		if probe {
			fmt.Printf("PROBE startup_main_window=%v\n", s.win != nil)
			// Exercise the same management-window action used by the native menu.
			for _, item := range s.trayItems(s.service.Status()) {
				if item.ID == "manage" {
					item.Click(item, nil)
				}
			}
		}
		s.refresh()
		s.workers.Add(1)
		go func() { defer s.workers.Done(); s.poll(ctx) }()
		if probe {
			go func() {
				time.Sleep(2 * time.Second)
				if data, e := s.win.CapturePage(); e == nil {
					_ = os.MkdirAll("tmp", 0700)
					_ = os.WriteFile("tmp/mygo-poc.png", data, 0600)
				} else {
					log.Print(e)
				}

				capture := func(name string, w *mygo.Window) {
					if data, e := w.CapturePage(); e == nil {
						_ = os.WriteFile("tmp/"+name+".png", data, 0600)
					}
				}
				mygo.RunOnMain(func() { s.showCLI() })
				time.Sleep(300 * time.Millisecond)
				capture("cli-install", s.cliWin)
				mygo.RunOnMain(func() { s.cliWin.Close(); mygo.Theme.SetSource(mygo.ThemeDark) })
				time.Sleep(300 * time.Millisecond)
				capture("dark", s.win)
				mygo.RunOnMain(func() { mygo.Theme.SetSource(mygo.ThemeSystem); s.win.SetSize(480, 360) })
				time.Sleep(250 * time.Millisecond)
				capture("narrow", s.win)
				mygo.RunOnMain(func() {
					s.win.SetSize(640, 480)
					s.tab = 1
					if len(s.runs) > 0 {
						s.selectedRun = s.runs[0].ID
					}
					s.win.Invalidate()
				})
				time.Sleep(1200 * time.Millisecond)
				capture("run-detail", s.win)
				var form *taskForm
				mygo.RunOnMain(func() { s.tab = 0; s.selectedRun = ""; s.win.Invalidate(); form = s.editTask(nil) })
				time.Sleep(300 * time.Millisecond)
				capture("task-sheet", form.win)
				mygo.RunOnMain(func() { form.win.Close(); s.win.Close(); fmt.Println("PROBE window_closed") })
				time.Sleep(time.Second)
				mygo.RunOnMain(func() { s.show(); fmt.Printf("PROBE reopened visible=%v\n", s.win.IsVisible()) })
				time.Sleep(time.Second)
				fmt.Println("PROBE quit_requested")
				appUI.Quit()
			}()
		}
	})
	if e := appUI.Run(); e != nil {
		return e
	}
	cancel()
	return err
}
func mustEvents(s *services.Service) []common.AttentionEvent { v, _ := s.Events(); return v }
func (s *windowState) show() {
	if s.win == nil || s.win.IsDestroyed() {
		s.win = mygo.NewWindow(mygo.WindowOptions{Title: "Apilot", Width: 640, Height: 480, MinWidth: 480, MinHeight: 360, StateKey: "main", Content: ui.View(s.view)})
		s.win.OnClosed(func() {
			s.operation.Invalidate()
			s.busy = false
			s.refreshState.Invalidate()
			s.logOperation.Invalidate()
			s.logState.Generation++
		})
	}
	s.win.Show()
}
func (s *windowState) update(fn func()) {
	mygo.RunOnMain(func() {
		if s.stopping {
			return
		}
		fn()
		if s.win != nil && !s.win.IsDestroyed() {
			s.win.Invalidate()
		}
		if s.attentionWin != nil && !s.attentionWin.IsDestroyed() {
			s.attentionWin.Invalidate()
		}
	})
}
func (s *windowState) background(fn func()) {
	if s.stopping {
		return
	}
	s.workers.Add(1)
	go func() { defer s.workers.Done(); fn() }()
}
func (s *windowState) action(fn func() error) {
	if s.stopping {
		return
	}
	token, ok := s.operation.Begin()
	if !ok {
		return
	}
	s.busy = true
	s.err = ""
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		err := fn()
		s.update(func() {
			if !s.operation.Complete(token) {
				return
			}
			s.busy = false
			if err != nil {
				s.err = err.Error()
			}
			s.refresh()
		})
	}()
}
func (s *windowState) refresh() {
	mygo.RunOnMain(func() {
		if s.stopping {
			return
		}
		token, ok := s.refreshState.Begin()
		if !ok {
			return
		}
		s.workers.Add(1)
		go func() {
			defer s.workers.Done()
			tasks, runs, observers := s.service.Tasks(), s.service.Runs(""), s.service.Observers()
			events, err := s.service.Events()
			status := s.service.Status()
			settings := s.service.Settings()
			s.update(func() {
				if !s.refreshState.Complete(token) {
					return
				}
				if err != nil {
					s.err = err.Error()
				} else {
					s.tasks, s.runs, s.observers, s.events = tasks, runs, observers, events
					if s.tray != nil {
						s.updateTray(status)
					}
					for _, candidate := range presentation.Notifications(settings, runs, events, s.seen) {
						s.sendNotification(candidate)
					}
				}
				if s.refreshState.Pending() {
					s.refresh()
				}
			})
		}()
	})
}
func (s *windowState) poll(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.update(func() { s.refresh(); s.readLogs() })
		}
	}
}
func (s *windowState) readLogs() {
	stream := []string{"stdout", "stderr"}[s.stream]
	key := s.selectedRun + stream
	if s.logKey != key {
		s.logKey = key
		s.logText = ""
		s.logOffset = 0
		s.logState.Generation++
		s.logOperation.Invalidate()
	}
	if s.selectedRun == "" {
		return
	}
	token, ok := s.logOperation.Begin()
	if !ok {
		return
	}
	selected := presentation.LogSelection{RunID: s.selectedRun, Stream: stream, Offset: s.logOffset, Generation: s.logState.Generation}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		chunk, err := s.service.Logs(selected.RunID, selected.Stream, selected.Offset, 65536)
		s.update(func() {
			if !s.logOperation.Complete(token) {
				return
			}
			current := presentation.LogSelection{RunID: s.selectedRun, Stream: []string{"stdout", "stderr"}[s.stream], Offset: s.logOffset, Generation: s.logState.Generation}
			if selected != current {
				return
			}
			if err != nil {
				s.err = err.Error()
				return
			}
			if s.followLogs && s.logScroll.Y >= s.logScroll.MaxY-1 {
				s.logScroll.Y = math.MaxFloat32
			}
			s.logText += string(chunk.Data)
			if len(s.logText) > 256*1024 {
				s.logText = s.logText[len(s.logText)-256*1024:]
			}
			s.logOffset = chunk.NextOffset
		})
	}()
}
func (s *windowState) updateTray(status map[string]any) {
	signature := fmt.Sprint(status) + s.err + fmt.Sprint(s.busy)
	for _, t := range s.tasks {
		signature += fmt.Sprintf("%s:%d;", t.ID, t.Revision)
	}
	for _, r := range s.runs {
		signature += r.ID + r.Status
	}
	for _, e := range s.events {
		signature += fmt.Sprintf("%s:%v", e.ID, e.AcknowledgedAt)
	}
	if signature == s.traySignature {
		return
	}
	s.traySignature = signature

	s.tray.SetMenu(mygo.NewMenu(s.trayItems(status)))
}

func statusText(v string) string {
	switch v {
	case "queued":
		return tr("queued")
	case "running":
		return tr("running")
	case "succeeded":
		return tr("succeeded")
	case "failed":
		return tr("failed")
	case "canceled":
		return tr("cancelled")
	case "interrupted":
		return tr("interrupted")
	case "skipped":
		return tr("skipped")
	}
	return v
}
func (s *windowState) view(c *ui.Context) {
	brandTheme(c)
	ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
		if ui.Tabs(c, &s.tab, tr("tasks"), tr("history"), tr("observers")).Changed() {
			s.selectedTask = ""
			s.selectedRun = ""
			s.selectedObserver = ""
			s.err = ""
		}
		if s.err != "" {
			ui.Text(c, s.err).TextColor(c.Theme().Danger)
		}
		if s.selectedRun != "" {
			s.runDetail(c)
			return
		}
		if s.selectedTask != "" {
			s.taskDetail(c)
			return
		}
		if s.tab == 0 {
			s.taskList(c)
		} else if s.tab == 1 {
			s.runList(c)
		} else {
			s.observerList(c)
		}
	})
}
func (s *windowState) taskList(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.TextInput(c, &s.query).Placeholder(tr("searchTasks")).Label(tr("searchTasks")).Grow(1)
		if ui.Button(c, "＋").Label(tr("newTask")).Tooltip(tr("newTask")).Clicked() {
			s.editTask(nil)
		}
	})
	if len(s.tasks) == 0 {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Image(c, appMark).Size(64, 64)
			ui.Text(c, tr("noTasks"))
			if ui.PrimaryButton(c, tr("newTask")).Clicked() {
				s.editTask(nil)
			}
		})
		return
	}
	matches := 0
	for _, t := range s.tasks {
		if contains(t.Name, s.query) {
			matches++
		}
	}
	if matches == 0 {
		ui.Text(c, tr("noMatchingTasks"))
		if ui.Button(c, tr("clearSearch")).Clicked() {
			s.query = ""
		}
		return
	}
	ui.Scroll(c).Grow(1).TrackScroll(&s.taskScroll).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for _, t := range s.tasks {
				t := t
				if !contains(t.Name, s.query) {
					continue
				}
				ui.Row(c.Key(t.ID)).FillWidth().Gap(8).Children(func() {
					entry := ui.ButtonBase(c.Key("open")).Label(t.Name).Tooltip(t.Name).Grow(1).Padding(8).Radius(6).Justify(ui.Start)
					if entry.Hovered() {
						entry.Background(c.Theme().SurfaceHover)
					}
					entry.Children(func() {
						ui.Column(c).FillWidth().Gap(4).Children(func() {
							ui.Row(c).FillWidth().Gap(8).Children(func() {
								ui.Text(c, t.Name).Grow(1)
								if t.LastRun != nil {
									ui.Text(c, statusText(t.LastRun.Status)).FontSize(12).TextColor(resultColor(c, t.LastRun.Status))
								}
							})
							ui.Text(c, planLabel(t)).FontSize(12).TextColor(c.Theme().TextMuted)
						})
					})
					if entry.Clicked() {
						s.selectedTask = t.ID
					}
					if ui.Button(c.Key("run"), "▶").Label(tr("runTask")).Tooltip(tr("runTask")).Disabled(s.busy).Clicked() {
						s.action(func() error { _, e := s.service.RunTask(t.ID, "manual"); return e })
					}
				})
			}
		})
	})
}
func (s *windowState) taskDetail(c *ui.Context) {
	var task *common.Task
	for _, t := range s.tasks {
		if t.ID == s.selectedTask {
			cp := t
			task = &cp
			break
		}
	}
	if task == nil {
		s.selectedTask = ""
		return
	}
	t := *task
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		if ui.Button(c, "‹").Label(tr("backToTasks")).Tooltip(tr("backToTasks")).Clicked() {
			s.selectedTask = ""
		}
		ui.Text(c, t.Name).Bold().Grow(1)
		if ui.Button(c, tr("edit")).Clicked() {
			s.editTask(&t)
		}
		if ui.PrimaryButton(c, tr("run")).Disabled(s.busy).Clicked() {
			s.action(func() error { _, e := s.service.RunTask(t.ID, "manual"); return e })
		}
	})
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, planLabel(t))
			on := t.Enabled && (t.Schedule == nil || t.Schedule.Enabled)
			if t.Schedule != nil && ui.Checkbox(c, &on, tr("automaticRuns")).Changed() {
				s.action(func() error { return s.setAutomatic(t) })
			}
			ui.Text(c, t.Executable).Selectable()
			for _, arg := range t.Args {
				ui.Text(c, arg).Selectable()
			}
			ui.Text(c, t.Cwd).TextColor(c.Theme().TextMuted).Selectable()
			if len(t.EnvKeys) > 0 {
				ui.Text(c, tr("environmentVariables")+strings.Join(t.EnvKeys, ", "))
			}
			for _, r := range s.runs {
				if r.TaskID == t.ID {
					r := r
					if ui.Button(c.Key(r.ID), statusText(r.Status)+" · "+r.CreatedAt.Local().Format("01-02 15:04:05")).FillWidth().Clicked() {
						s.selectedRun = r.ID
					}
				}
			}
			if ui.Button(c, tr("deleteTask2")).Disabled(s.busy).Clicked() {
				go func() {
					result, e := mygo.Dialog.Message(mygo.MessageOptions{Parent: s.win, Type: mygo.MessageWarning, Message: fmt.Sprintf(tr("deleteTaskS"), t.Name), Detail: tr("executionHistoryAndWorkingDirectoriesAreKept"), Buttons: []string{tr("cancel"), tr("deleteTask")}})
					if e == nil && result.Button == 1 {
						s.win.Update(func() { s.action(func() error { return s.service.DeleteTask(t.ID) }) })
					}
				}()
			}
		})
	})
}
func (s *windowState) runList(c *ui.Context) {
	if len(s.runs) == 0 {
		ui.Text(c, tr("noRunsYet"))
		return
	}
	ui.Scroll(c).Grow(1).TrackScroll(&s.runScroll).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for _, r := range s.runs {
				r := r
				entry := ui.ButtonBase(c.Key(r.ID)).Label(r.TaskName + " " + statusText(r.Status)).FillWidth().Padding(8).Justify(ui.Start)
				if entry.Hovered() {
					entry.Background(c.Theme().SurfaceHover)
				}
				entry.Children(func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() {
						ui.Text(c, r.TaskName+" · "+statusText(r.Status)).TextColor(resultColor(c, r.Status))
						ui.Text(c, r.CreatedAt.Local().Format("01-02 15:04:05")+" · "+triggerText(r.Trigger)).FontSize(12).TextColor(c.Theme().TextMuted)
					})
				})
				if entry.Clicked() {
					s.selectedRun = r.ID
				}
			}
		})
	})
}
func (s *windowState) runDetail(c *ui.Context) {
	var run *common.Run
	for _, r := range s.runs {
		if r.ID == s.selectedRun {
			cp := r
			run = &cp
			break
		}
	}
	if run == nil {
		s.selectedRun = ""
		return
	}
	r := *run
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		if ui.Button(c, "‹").Label(tr("back")).Tooltip(tr("back")).Clicked() {
			s.selectedRun = ""
		}
		ui.Text(c, r.TaskName+" · "+statusText(r.Status)).TextColor(resultColor(c, r.Status)).Grow(1).Bold()
		if r.Active() {
			if ui.Button(c, "■").Label(tr("stopRun")).Tooltip(tr("stopRun")).Clicked() {
				s.action(func() error { _, e := s.service.StopRun(r.ID); return e })
			}
		}
	})
	meta := r.CreatedAt.Local().Format("2006-01-02 15:04:05")
	if r.ExitCode != nil {
		meta += fmt.Sprintf(tr("exitCodeD"), *r.ExitCode)
	}
	if r.DurationMS != nil {
		if *r.DurationMS < 1000 {
			meta += fmt.Sprintf(" · %dms", *r.DurationMS)
		} else {
			meta += fmt.Sprintf(" · %.1fs", float64(*r.DurationMS)/1000)
		}
	}
	ui.Text(c, meta).FontSize(12).TextColor(c.Theme().TextMuted)
	if r.Error != "" && !(r.ErrorCode == "COMMAND_FAILED" && r.ExitCode != nil) {
		ui.Text(c, r.Error).TextColor(c.Theme().Danger)
	}
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Tabs(c, &s.stream, tr("standardOutput"), tr("standardError")).Grow(1)
		if ui.Button(c, tr("copy")).Clicked() {
			mygo.Clipboard.WriteText(s.logText)
		}
		if ui.Button(c, tr("openLogs")).Clicked() {
			name := []string{"stdout", "stderr"}[s.stream]
			s.action(func() error { return mygo.Shell.OpenPath(filepath.Join(r.LogPath, name+".log")) })
		}
	})
	if ui.Checkbox(c, &s.followLogs, tr("followOutput")).Changed() && s.followLogs {
		s.logScroll.Y = math.MaxFloat32
	}
	ui.ScrollBoth(c).Grow(1).TrackScroll(&s.logScroll).Children(func() { ui.Text(c, s.logText).Font("monospace").FontSize(12).Selectable().NoWrap() })
	for _, e := range s.events {
		if e.RunID == r.ID && e.AcknowledgedAt == nil {
			e := e
			if ui.Button(c, tr("markAsRead")).Clicked() {
				s.action(func() error { _, err := s.service.AckEvent(e.ID); return err })
			}
			break
		}
	}
}

func triggerText(v string) string {
	switch v {
	case "cli":
		return "CLI"
	case "manual":
		return tr("manual")
	case "scheduled":
		return tr("scheduled")
	}
	return v
}

func (s *windowState) selectEvent(e common.AttentionEvent) {
	target := presentation.EventTarget(e)
	s.selectedTask = ""
	s.selectedRun, s.selectedObserver, s.tab = target.RunID, target.ObserverID, target.Tab
}

func taskActive(runs []common.Run, id string) bool { return presentation.TaskActive(runs, id) }
func (s *windowState) setAutomatic(t common.Task) error {
	if t.Schedule == nil {
		return common.Err("INVALID_ARGUMENT", "Task has no schedule")
	}
	_, err := s.service.SetAutomatic(t.ID, !(t.Enabled && t.Schedule.Enabled), t.Revision)
	return err
}

func (s *windowState) setAppMenu() {
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Apilot", Submenu: []*mygo.MenuItem{{Role: mygo.RoleAbout}, {Label: tr("settings2"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { s.showSettings() }}, mygo.Separator(), {Role: mygo.RoleQuit}}},
		{Role: mygo.RoleEditMenu}, {Label: tr("window"), Submenu: []*mygo.MenuItem{{Label: tr("openApilot"), Click: func(*mygo.MenuItem, *mygo.Window) { s.show() }}}},
	}))
}
