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

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/presentation"
	"github.com/alswl/apilot/core/pkg/server"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/alswl/apilot/desktop/internal/ui/workbench"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type automationBackend interface {
	AutomationSnapshot() (services.AutomationSnapshot, error)
	SaveTask(string, common.TaskInput, int64) (common.Task, error)
	DeleteTask(string) error
	SetAutomatic(string, bool, int64) (common.Task, error)
	RunTask(string, string) (common.Run, error)
	StopRun(string) (common.Run, error)
	Logs(string, string, int64, int) (common.LogChunk, error)
	SaveObserver(string, common.ObserverInput, int64) (common.Observer, error)
	DeleteObserver(string) error
	SetObserverEnabled(string, bool) (common.Observer, error)
	AckEvent(string) (common.AttentionEvent, error)
	SaveSettings(common.Settings) (common.Settings, error)
	AllowNotification(string) (bool, error)
	Suspend()
	Resume()
}

type windowState struct {
	workspace        *workbench.HostedWorkspace
	settings         common.Settings
	cliPanelWindow   *mygo.Window
	cliPanel         *cliSettings
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
	service          automationBackend
	win              *mygo.Window
	settingsWin      *mygo.Window
	taskEditor       *taskForm
	observerEditor   *mygo.Window
	confirming       bool
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

func Run(runtime *server.Runtime, remote *services.Remote, reveal, probe bool, keybindingsPath, language string) error {
	mygo.App.SetLanguages("en", "zh-Hans")
	var service automationBackend = services.AutomationRemote{Remote: remote}
	if runtime != nil {
		service = runtime.Service
	}
	first, err := service.AutomationSnapshot()
	if err != nil {
		return err
	}
	s := &windowState{service: service, settings: first.Settings, followLogs: true, seen: presentation.InitialSeen(first.Runs, first.Events)}
	appUI := mygo.App
	appUI.SetName("Apilot")
	appUI.SetActivationPolicy(mygo.ActivationPolicyRegular)
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
			if runtime != nil {
				go runtime.StopAdmission()
			}
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
			if runtime != nil {
				runtime.StopAdmission()
				runtime.StopIPC()
			}
			s.workers.Wait()
			if runtime != nil {
				_ = runtime.Close()
			}
			mygo.RunOnMain(func() { allowQuit = true; appUI.Quit() })
		}()
	})
	appUI.OnActivate(func(visible bool) {
		if s.tray != nil && !s.stopping && (!visible || s.win == nil || !s.win.IsVisible()) {
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
			if s.workspace != nil {
				s.workspace.UpdateLocale(locale)
				s.workspace.InstallMenu()
			}
			s.traySignature = ""
			s.refresh()
		})
		s.workspace, err = workbench.NewHostedWorkspace(remote, keybindingsPath, language, workbench.HostPanels{
			Automation:        s.view,
			RunningAutomation: s.runningAutomation,
			PreferencePanels: []workbench.PreferencePanel{
				{Title: func() string { return tr("automationSettings") }, View: s.automationPreferences()},
				{Title: func() string { return tr("commandLineSettings") }, View: func(c *ui.Context, w *mygo.Window) { s.cliPreferences(c, w) }},
			},
		})
		if err != nil {
			allowQuit = true
			appUI.Quit()
			return
		}
		displayLocale.Store(s.workspace.EffectiveLocale())
		s.workspace.InstallMenu()
		s.tray, err = mygo.NewTray(mygo.TrayOptions{ID: "apilot", Icon: menuBarIcon, IconIsTemplate: true, ToolTip: "Apilot"})
		if err != nil {
			allowQuit = true
			appUI.Quit()
			return
		}
		log.Print("tray created")
		if reveal {
			s.show()
		}
		if probe {
			fmt.Printf("PROBE startup_main_window=%v\n", s.win != nil)
			// Exercise the same management-window action used by the native menu.
			for _, item := range s.trayItems(first.Status) {
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
				mygo.RunOnMain(func() {
					s.showCLI()
					settings := s.settingsWin
					s.showSettings()
					if s.settingsWin != settings {
						panic("duplicate settings window")
					}
				})
				time.Sleep(300 * time.Millisecond)
				capture("cli-install", s.settingsWin)
				mygo.RunOnMain(func() { s.settingsWin.Close(); mygo.Theme.SetSource(mygo.ThemeDark) })
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
				mygo.RunOnMain(func() {
					s.tab = 0
					s.selectedRun = ""
					s.win.Invalidate()
					form = s.editTask(nil)
					if s.editTask(nil) != form {
						panic("duplicate task editor")
					}
					s.editObserver(nil)
					if s.observerEditor != nil {
						panic("stacked observer editor")
					}
					s.openHistory("")
					if s.tab != 0 {
						panic("navigation replaced editor context")
					}
					s.win.Close()
					if s.win.IsDestroyed() {
						panic("parent closed with draft")
					}
					fmt.Println("PROBE editor_reused context_preserved parent_retained")
				})
				time.Sleep(300 * time.Millisecond)
				capture("task-sheet", form.win)
				mygo.RunOnMain(func() {
					form.win.Close()
					s.editObserver(nil)
					observer := s.observerEditor
					s.editObserver(nil)
					if s.observerEditor != observer {
						panic("duplicate observer editor")
					}
					observer.Close()
					s.win.Close()
					fmt.Println("PROBE window_closed")
				})
				time.Sleep(time.Second)
				mygo.RunOnMain(func() { s.show(); fmt.Printf("PROBE reopened visible=%v\n", s.win.IsVisible()) })
				time.Sleep(time.Second)
				mygo.RunOnMain(func() { s.editTask(nil) })
				fmt.Println("PROBE quit_requested_with_editor")
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
func (s *windowState) show() {
	if s.workspace == nil {
		return
	}
	if s.win == nil || s.win.IsDestroyed() {
		s.win = s.workspace.Open()
		s.win.OnClose(func(e *mygo.CloseEvent) {
			if !s.stopping && s.focusEditor() {
				e.PreventDefault()
			}
		})
		s.win.OnClosed(func() {
			s.operation.Invalidate()
			s.busy = false
			s.refreshState.Invalidate()
			s.logOperation.Invalidate()
			s.logState.Generation++
		})
	} else {
		s.workspace.Open()
	}
	s.focusEditor()
}

func (s *windowState) showAutomation() {
	if s.workspace != nil {
		s.workspace.ShowAutomation()
	}
	s.show()
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
			snapshot, err := s.service.AutomationSnapshot()
			s.update(func() {
				if !s.refreshState.Complete(token) {
					return
				}
				if err != nil {
					s.err = err.Error()
				} else {
					s.tasks, s.runs, s.observers, s.events, s.settings = snapshot.Tasks, snapshot.Runs, snapshot.Observers, snapshot.Events, snapshot.Settings
					if s.tray != nil {
						s.updateTray(snapshot.Status)
					}
					for _, candidate := range presentation.Notifications(snapshot.Settings, snapshot.Runs, snapshot.Events, s.seen) {
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
	ui.Column(c).Fill().Gap(12).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			if ui.Tabs(c, &s.tab, tr("tasks"), tr("history"), tr("observers"), tr("attention")).Grow(1).Changed() {
				s.selectedTask = ""
				s.selectedRun = ""
				s.selectedObserver = ""
				s.err = ""
			}
			ui.Text(c, "v"+common.Version).FontSize(11).TextColor(c.Theme().TextMuted).Shrink(0).Selectable()
		})
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
		} else if s.tab == 2 {
			s.observerList(c)
		} else {
			s.attentionList(c)
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
					item := components.ListItem{Title: t.Name, Subtitle: planLabel(t), Selected: s.selectedTask == t.ID}
					if t.LastRun != nil {
						item.Status = components.Status{Text: statusText(t.LastRun.Status), Color: resultColor(c, t.LastRun.Status)}
					}
					if components.ListRow(c.Key("open"), item) {
						s.selectedTask = t.ID
					}
					if components.IconButton(c.Key("run"), "play.fill", tr("runTask")+" — "+t.Name, s.busy).Clicked() {
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
				s.confirmDelete(fmt.Sprintf(tr("deleteTaskS"), t.Name), tr("executionHistoryAndWorkingDirectoriesAreKept"), tr("deleteTask"), func() error { return s.service.DeleteTask(t.ID) })
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
	if s.focusEditor() {
		return
	}
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

func (s *windowState) runningAutomation() int {
	running := 0
	for _, item := range s.runs {
		if item.Active() {
			running++
		}
	}
	return running
}
