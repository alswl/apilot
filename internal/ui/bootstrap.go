package ui

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/ipc"
	"github.com/alswl/apilot/internal/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type windowState struct {
	traySignature    string
	taskScroll       ui.ScrollState
	runScroll        ui.ScrollState
	logScroll        ui.ScrollState
	followLogs       bool
	observers        []model.Observer
	selectedObserver string
	service          *app.Service
	win              *mygo.Window
	settingsWin      *mygo.Window
	cliWin           *mygo.Window
	attentionWin     *mygo.Window
	tray             *mygo.Tray
	tab              int
	tasks            []model.Task
	runs             []model.Run
	events           []model.AttentionEvent
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

func Run(probe bool) error {
	service, err := app.Open(app.DataDir())
	if err != nil {
		return err
	}
	defer service.Close()
	server, err := ipc.Listen(service, app.SocketPath(app.DataDir()))
	if err != nil {
		return err
	}
	defer server.Close()
	s := &windowState{service: service, followLogs: true}
	appUI := mygo.App
	appUI.SetName("Apilot")
	appUI.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var allowQuit bool
	appUI.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if allowQuit {
			return
		}
		if e.SessionEnding {
			cancel()
			service.Suspend()
			return
		}
		e.PreventDefault()
		if s.busy && s.err == "正在退出…" {
			return
		}
		s.err = "正在退出…"
		s.busy = true
		cancel()
		go func() { server.Close(); _ = service.Close(); mygo.RunOnMain(func() { allowQuit = true; appUI.Quit() }) }()
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
		appUI.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Apilot", Submenu: []*mygo.MenuItem{{Role: mygo.RoleAbout}, {Label: "设置…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { s.showSettings() }}, mygo.Separator(), {Role: mygo.RoleQuit}}},
			{Role: mygo.RoleEditMenu}, {Label: "窗口", Submenu: []*mygo.MenuItem{{Label: "打开 Apilot", Click: func(*mygo.MenuItem, *mygo.Window) { s.show() }}}},
		}))
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
		go s.poll(ctx)
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
	return err
}
func (s *windowState) show() {
	if s.win == nil || s.win.IsDestroyed() {
		s.win = mygo.NewWindow(mygo.WindowOptions{Title: "Apilot", Width: 640, Height: 480, MinWidth: 480, MinHeight: 360, StateKey: "main", Content: ui.View(s.view)})
	}
	s.win.Show()
}
func (s *windowState) update(fn func()) {
	mygo.RunOnMain(func() {
		fn()
		if s.win != nil && !s.win.IsDestroyed() {
			s.win.Invalidate()
		}
		if s.attentionWin != nil && !s.attentionWin.IsDestroyed() {
			s.attentionWin.Invalidate()
		}
	})
}
func (s *windowState) action(fn func() error) {
	if s.busy {
		return
	}
	s.busy = true
	s.err = ""
	go func() {
		err := fn()
		s.update(func() {
			s.busy = false
			if err != nil {
				s.err = err.Error()
			}
		})
		s.refresh()
	}()
}
func (s *windowState) refresh() {
	tasks := s.service.Tasks()
	runs := s.service.Runs("")
	observers := s.service.Observers()
	events, e := s.service.Events()
	status := s.service.Status()
	if e != nil {
		return
	}
	s.update(func() {
		s.tasks = tasks
		s.runs = runs
		s.events = events
		s.observers = observers
		s.updateTray(status)
	})
}
func (s *windowState) poll(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	seen := map[string]bool{}
	for _, r := range s.service.Runs("") {
		if !r.Active() {
			seen[r.ID] = true
		}
	}
	events, _ := s.service.Events()
	for _, e := range events {
		seen[e.ID] = true
	}
	lastPrune := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refresh()
			s.notify(ctx, seen)
			if time.Since(lastPrune) > 24*time.Hour {
				_ = s.service.Prune()
				lastPrune = time.Now()
			}
			var runID string
			var stream int
			var offset int64
			var key string
			mygo.RunOnMain(func() {
				runID = s.selectedRun
				stream = s.stream
				key = runID + []string{"stdout", "stderr"}[stream]
				if s.logKey != key {
					s.logText = ""
					s.logOffset = 0
					s.logKey = key
				}
				offset = s.logOffset
			})
			if runID != "" {
				name := []string{"stdout", "stderr"}[stream]
				chunk, e := s.service.Logs(runID, name, offset, 65536)
				s.update(func() {
					if e != nil {
						s.err = e.Error()
					} else if s.logKey == key {
						if s.followLogs && s.logScroll.Y >= s.logScroll.MaxY-1 {
							s.logScroll.Y = math.MaxFloat32
						}
						s.logText += string(chunk.Data)
						if len(s.logText) > 256*1024 {
							s.logText = s.logText[len(s.logText)-256*1024:]
						}
						s.logOffset = chunk.NextOffset
					}
				})
			}
		}
	}
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
		return "准备中"
	case "running":
		return "运行中"
	case "succeeded":
		return "成功"
	case "failed":
		return "失败"
	case "canceled":
		return "已取消"
	case "interrupted":
		return "已中断"
	case "skipped":
		return "已跳过"
	}
	return v
}
func (s *windowState) view(c *ui.Context) {
	brandTheme(c)
	ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
		if ui.Tabs(c, &s.tab, "任务", "历史", "观察").Changed() {
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
		ui.TextInput(c, &s.query).Placeholder("搜索任务").Label("搜索任务").Grow(1)
		if ui.Button(c, "＋").Label("新建任务").Tooltip("新建任务").Clicked() {
			s.editTask(nil)
		}
	})
	if len(s.tasks) == 0 {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Image(c, appMark).Size(64, 64)
			ui.Text(c, "暂无任务")
			if ui.PrimaryButton(c, "新建任务").Clicked() {
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
		ui.Text(c, "无匹配任务")
		if ui.Button(c, "清除搜索").Clicked() {
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
					if ui.Button(c.Key("run"), "▶").Label("运行任务").Tooltip("运行任务").Disabled(s.busy).Clicked() {
						s.action(func() error { _, e := s.service.RunTask(t.ID, "manual"); return e })
					}
				})
			}
		})
	})
}
func (s *windowState) taskDetail(c *ui.Context) {
	var task *model.Task
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
		if ui.Button(c, "‹").Label("返回任务").Tooltip("返回任务").Clicked() {
			s.selectedTask = ""
		}
		ui.Text(c, t.Name).Bold().Grow(1)
		if ui.Button(c, "编辑").Clicked() {
			s.editTask(&t)
		}
		if ui.PrimaryButton(c, "运行").Disabled(s.busy).Clicked() {
			s.action(func() error { _, e := s.service.RunTask(t.ID, "manual"); return e })
		}
	})
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			ui.Text(c, planLabel(t))
			on := t.Enabled && (t.Schedule == nil || t.Schedule.Enabled)
			if t.Schedule != nil && ui.Checkbox(c, &on, "自动运行").Changed() {
				s.action(func() error { return s.setAutomatic(t) })
			}
			ui.Text(c, t.Executable).Selectable()
			for _, arg := range t.Args {
				ui.Text(c, arg).Selectable()
			}
			ui.Text(c, t.Cwd).TextColor(c.Theme().TextMuted).Selectable()
			if len(t.EnvKeys) > 0 {
				ui.Text(c, "环境变量："+strings.Join(t.EnvKeys, ", "))
			}
			for _, r := range s.runs {
				if r.TaskID == t.ID {
					r := r
					if ui.Button(c.Key(r.ID), statusText(r.Status)+" · "+r.CreatedAt.Local().Format("01-02 15:04:05")).FillWidth().Clicked() {
						s.selectedRun = r.ID
					}
				}
			}
			if ui.Button(c, "删除任务…").Disabled(s.busy).Clicked() {
				go func() {
					result, e := mygo.Dialog.Message(mygo.MessageOptions{Parent: s.win, Type: mygo.MessageWarning, Message: "删除任务「" + t.Name + "」？", Detail: "保留执行历史和工作目录。", Buttons: []string{"取消", "删除任务"}})
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
		ui.Text(c, "暂无执行记录")
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
	var run *model.Run
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
		if ui.Button(c, "‹").Label("返回").Tooltip("返回").Clicked() {
			s.selectedRun = ""
		}
		ui.Text(c, r.TaskName+" · "+statusText(r.Status)).TextColor(resultColor(c, r.Status)).Grow(1).Bold()
		if r.Active() {
			if ui.Button(c, "■").Label("停止运行").Tooltip("停止运行").Clicked() {
				s.action(func() error { _, e := s.service.StopRun(r.ID); return e })
			}
		}
	})
	meta := r.CreatedAt.Local().Format("2006-01-02 15:04:05")
	if r.ExitCode != nil {
		meta += fmt.Sprintf(" · 退出码 %d", *r.ExitCode)
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
		ui.Tabs(c, &s.stream, "标准输出", "错误输出").Grow(1)
		if ui.Button(c, "复制").Clicked() {
			mygo.Clipboard.WriteText(s.logText)
		}
		if ui.Button(c, "打开日志").Clicked() {
			name := []string{"stdout", "stderr"}[s.stream]
			s.action(func() error { return mygo.Shell.OpenPath(filepath.Join(r.LogPath, name+".log")) })
		}
	})
	if ui.Checkbox(c, &s.followLogs, "跟随输出").Changed() && s.followLogs {
		s.logScroll.Y = math.MaxFloat32
	}
	ui.ScrollBoth(c).Grow(1).TrackScroll(&s.logScroll).Children(func() { ui.Text(c, s.logText).Font("monospace").FontSize(12).Selectable().NoWrap() })
	for _, e := range s.events {
		if e.RunID == r.ID && e.AcknowledgedAt == nil {
			e := e
			if ui.Button(c, "标为已读").Clicked() {
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
		return "手动"
	case "scheduled":
		return "计划"
	}
	return v
}

func (s *windowState) selectEvent(e model.AttentionEvent) {
	s.selectedTask = ""
	s.selectedObserver = ""
	s.selectedRun = e.RunID
	s.tab = 1
	if e.SourceType == "observer" {
		s.selectedRun = ""
		s.selectedObserver = e.SourceID
		s.tab = 2
	}
}

func taskActive(runs []model.Run, taskID string) bool {
	for _, r := range runs {
		if r.TaskID == taskID && r.Active() {
			return true
		}
	}
	return false
}

func (s *windowState) setAutomatic(t model.Task) error {
	plan := *t.Schedule
	on := !(t.Enabled && plan.Enabled)
	plan.Enabled = on
	_, err := s.service.SaveTask(t.ID, model.TaskInput{
		Name: t.Name, Description: t.Description, Executable: t.Executable,
		Args: t.Args, Cwd: t.Cwd, TimeoutMS: t.TimeoutMS, Enabled: on, Schedule: &plan,
	}, t.Revision)
	return err
}
