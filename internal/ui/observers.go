package ui

import (
	"github.com/alswl/apilot/internal/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"strings"
	"time"
)

func (s *windowState) observerList(c *ui.Context) {
	if s.selectedObserver != "" {
		s.observerDetail(c)
		return
	}
	ui.Row(c).FillWidth().Children(func() {
		ui.Spacer(c).Grow(1)
		if ui.Button(c, "＋").Label("新建观察器").Tooltip("新建观察器").Clicked() {
			s.editObserver(nil)
		}
	})
	if len(s.observers) == 0 {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Text(c, "暂无观察器")
			if ui.PrimaryButton(c, "新建观察器").Clicked() {
				s.editObserver(nil)
			}
		})
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).FillWidth().Gap(6).Children(func() {
			for _, v := range s.observers {
				v := v
				label := healthText(v.Health)
				if !v.Enabled {
					label += " · 已停用"
				}
				entry := ui.ButtonBase(c.Key(v.ID)).Label(v.Name).FillWidth().Padding(8).Justify(ui.Start)
				if entry.Hovered() {
					entry.Background(c.Theme().SurfaceHover)
				}
				entry.Children(func() {
					ui.Column(c).FillWidth().Gap(4).Children(func() { ui.Text(c, v.Name); ui.Text(c, label).FontSize(12).TextColor(c.Theme().TextMuted) })
				})
				if entry.Clicked() {
					s.selectedObserver = v.ID
				}
			}
		})
	})
}
func healthText(v string) string {
	switch v {
	case "healthy":
		return "正常"
	case "unhealthy":
		return "异常"
	default:
		return "尚未检查"
	}
}
func (s *windowState) observerDetail(c *ui.Context) {
	var observer *model.Observer
	for _, v := range s.observers {
		if v.ID == s.selectedObserver {
			cp := v
			observer = &cp
			break
		}
	}
	if observer == nil {
		s.selectedObserver = ""
		return
	}
	v := *observer
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		if ui.Button(c, "‹").Label("返回观察器").Tooltip("返回观察器").Clicked() {
			s.selectedObserver = ""
		}
		ui.Text(c, v.Name).Grow(1).Bold()
		if ui.Button(c, "编辑").Clicked() {
			s.editObserver(&v)
		}
	})
	ui.Text(c, healthText(v.Health))
	if len(v.EnvKeys) > 0 {
		ui.Text(c, "环境变量："+strings.Join(v.EnvKeys, ", "))
	}
	on := v.Enabled
	if ui.Checkbox(c, &on, "自动检查").Changed() {
		s.action(func() error { _, e := s.service.SetObserverEnabled(v.ID, on); return e })
	}
	if v.LastCheckedAt != nil {
		ui.Text(c, "上次检查 "+v.LastCheckedAt.Local().Format("01-02 15:04:05")).FontSize(12)
	}
	if v.Error != "" {
		ui.Text(c, v.Error).TextColor(c.Theme().Danger)
	}
	text := v.Result
	ui.TextArea(c, &text).ReadOnly(true).Grow(1).Label("最近观察结果")
	for _, event := range s.events {
		if event.SourceType == "observer" && event.SourceID == v.ID && event.AcknowledgedAt == nil {
			event := event
			if ui.Button(c, "标为已读").Clicked() {
				s.action(func() error { _, e := s.service.AckEvent(event.ID); return e })
			}
			break
		}
	}
	if ui.Button(c, "删除观察器…").Clicked() {
		go func() {
			result, e := mygo.Dialog.Message(mygo.MessageOptions{Parent: s.win, Type: mygo.MessageWarning, Message: "删除观察器「" + v.Name + "」？", Detail: "停止后续检查。不会操作被观察的工具。", Buttons: []string{"取消", "删除观察器"}})
			if e == nil && result.Button == 1 {
				s.update(func() { s.action(func() error { return s.service.DeleteObserver(v.ID) }) })
			}
		}()
	}
}
func (s *windowState) editObserver(v *model.Observer) {
	home, _ := os.UserHomeDir()
	in := model.ObserverInput{Cwd: home, IntervalMS: 30000, TimeoutMS: 10000, Enabled: true, OutputMode: "text"}
	ref := ""
	revision := int64(0)
	title := "新建观察器"
	if v != nil {
		title = "编辑观察器"
		ref = v.ID
		revision = v.Revision
		in = model.ObserverInput{Name: v.Name, Executable: v.Executable, Args: v.Args, Cwd: v.Cwd, IntervalMS: v.IntervalMS, TimeoutMS: v.TimeoutMS, Enabled: v.Enabled, OutputMode: v.OutputMode}
	}
	args := strings.Join(in.Args, "\n")
	interval := time.Duration(in.IntervalMS * int64(time.Millisecond)).String()
	timeout := time.Duration(in.TimeoutMS * int64(time.Millisecond)).String()
	format := 0
	if in.OutputMode == "json" {
		format = 1
	}
	message := ""
	busy := false
	advanced := false
	env, unset := "", ""
	var win *mygo.Window
	win = mygo.NewWindow(mygo.WindowOptions{Title: title, Width: 540, Height: 500, MinWidth: 480, MinHeight: 360, Parent: s.win, Modal: true, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Text(c, title).Bold().FontSize(17)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					ui.Field(c, "名称", func() { ui.TextInput(c, &in.Name).FillWidth() })
					ui.Field(c, "可执行文件", func() { ui.TextInput(c, &in.Executable).FillWidth() })
					ui.Field(c, "参数（每行一个）", func() { ui.TextArea(c, &args).Height(60).FillWidth() })
					ui.Field(c, "工作目录", func() { ui.TextInput(c, &in.Cwd).FillWidth() })
					ui.Field(c, "检查间隔", func() { ui.TextInput(c, &interval).FillWidth() })
					ui.Collapsible(c, "高级", &advanced, func() {
						ui.Field(c, "超时", func() { ui.TextInput(c, &timeout).FillWidth() })
						ui.Tabs(c, &format, "文本", "JSON")
						ui.Checkbox(c, &in.Enabled, "自动检查")
						ui.Field(c, "新增环境变量（每行 KEY=VALUE）", func() { ui.TextArea(c, &env).Height(60).FillWidth() })
						ui.Field(c, "移除环境变量（每行一个名称）", func() { ui.TextArea(c, &unset).Height(50).FillWidth() })
					})
				})
			})
			if message != "" {
				ui.Text(c, message).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Disabled(busy).Clicked() {
					win.Close()
				}
				if ui.PrimaryButton(c, "保存").Disabled(busy).Clicked() {
					d, e := time.ParseDuration(interval)
					if e != nil {
						message = "检查间隔格式无效"
						return
					}
					t, e := time.ParseDuration(timeout)
					if e != nil {
						message = "超时格式无效"
						return
					}
					copy := in
					copy.EnvSet = map[string]string{}
					copy.EnvUnset = nil
					for _, line := range strings.Split(env, "\n") {
						if line == "" {
							continue
						}
						k, v, ok := strings.Cut(line, "=")
						if !ok || k == "" {
							message = "环境变量格式应为 KEY=VALUE"
							return
						}
						copy.EnvSet[k] = v
					}
					for _, line := range strings.Split(unset, "\n") {
						if line != "" {
							copy.EnvUnset = append(copy.EnvUnset, line)
						}
					}
					copy.IntervalMS = d.Milliseconds()
					copy.TimeoutMS = t.Milliseconds()
					copy.Args = []string{}
					if args != "" {
						copy.Args = strings.Split(args, "\n")
					}
					copy.OutputMode = []string{"text", "json"}[format]
					busy = true
					go func() {
						_, e := s.service.SaveObserver(ref, copy, revision)
						win.Update(func() {
							busy = false
							if e != nil {
								message = e.Error()
							} else {
								win.Close()
								go s.refresh()
							}
						})
					}()
				}
			})
		})
	})})
}
