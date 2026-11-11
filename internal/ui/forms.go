package ui

import (
	"github.com/alswl/apilot/internal/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"os"
	"strconv"
	"strings"
	"time"
)

func contains(s, q string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(q)) }
func planLabel(t model.Task) string {
	if t.Schedule == nil {
		return "手动运行"
	}
	label := t.Schedule.Expression
	if t.Schedule.Type == "interval" {
		label = "每 " + label
	}
	if !t.Enabled || !t.Schedule.Enabled {
		return label + " · 自动运行已关闭"
	}
	if t.Schedule.NextRunAt != nil {
		label += " · 下次 " + t.Schedule.NextRunAt.Local().Format("01-02 15:04:05")
	}
	return label
}

type taskForm struct {
	win                                                         *mygo.Window
	input                                                       model.TaskInput
	id                                                          string
	revision                                                    int64
	mode                                                        int
	plan                                                        int
	command, args, expression, zone, timeout, env, unset, error string
	advanced, busy                                              bool
}

func (s *windowState) editTask(t *model.Task) *taskForm {
	home, _ := os.UserHomeDir()
	settings := s.service.Settings()
	f := &taskForm{input: model.TaskInput{Enabled: true, Cwd: home, TimeoutMS: settings.DefaultTimeoutMS}, zone: "Local", timeout: time.Duration(settings.DefaultTimeoutMS * int64(time.Millisecond)).String()}
	title := "新建任务"
	if t != nil {
		title = "编辑任务"
		f.id = t.ID
		f.revision = t.Revision
		f.input = model.TaskInput{Name: t.Name, Description: t.Description, Executable: t.Executable, Args: t.Args, Cwd: t.Cwd, Enabled: t.Enabled, TimeoutMS: t.TimeoutMS}
		f.command = t.Executable
		f.args = strings.Join(t.Args, "\n")
		f.timeout = time.Duration(t.TimeoutMS * int64(time.Millisecond)).String()
		if t.Executable == "/bin/zsh" && len(t.Args) == 2 && t.Args[0] == "-lc" {
			f.mode = 1
			f.command = t.Args[1]
			f.args = ""
		}
		if t.Schedule != nil {
			f.expression = t.Schedule.Expression
			f.zone = t.Schedule.Timezone
			f.plan = 1
			if t.Schedule.Type == "interval" {
				f.plan = 2
			}
			cp := *t.Schedule
			f.input.Schedule = &cp
		}
	}
	f.win = mygo.NewWindow(mygo.WindowOptions{Title: title, Width: 560, Height: 520, MinWidth: 480, MinHeight: 360, Parent: s.win, Modal: true, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Text(c, title).Bold().FontSize(17)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					ui.Field(c, "名称", func() { ui.TextInput(c, &f.input.Name).FillWidth() })
					if ui.Tabs(c, &f.mode, "直接命令", "Shell").Changed() {
						f.command = ""
						f.args = ""
					}
					label := "可执行文件"
					if f.mode == 1 {
						label = "Shell 命令"
					}
					ui.Field(c, label, func() {
						if f.mode == 1 {
							ui.TextArea(c, &f.command).Height(70).FillWidth()
						} else {
							ui.TextInput(c, &f.command).Placeholder("/bin/echo").FillWidth()
						}
					})
					if f.mode == 0 {
						ui.Field(c, "参数（每行一个）", func() { ui.TextArea(c, &f.args).Height(60).FillWidth() })
					}
					ui.Field(c, "工作目录", func() { ui.TextInput(c, &f.input.Cwd).FillWidth() })
					ui.Tabs(c, &f.plan, "手动", "定时", "间隔")
					if f.plan > 0 {
						label := "Cron 表达式"
						example := "0 9 * * 1-5"
						if f.plan == 2 {
							label = "执行间隔"
							example = "30m"
						}
						ui.Field(c, label, func() { ui.TextInput(c, &f.expression).Placeholder(example).FillWidth() })
					}
					ui.Collapsible(c, "高级", &f.advanced, func() {
						ui.Column(c).FillWidth().Gap(10).Children(func() {
							ui.Field(c, "说明", func() { ui.TextInput(c, &f.input.Description).FillWidth() })
							ui.Field(c, "超时", func() { ui.TextInput(c, &f.timeout).FillWidth() })
							if f.plan == 1 {
								ui.Field(c, "时区", func() { ui.TextInput(c, &f.zone).FillWidth() })
							}
							ui.Field(c, "新增环境变量（每行 KEY=VALUE）", func() { ui.TextArea(c, &f.env).Height(60).FillWidth() })
							ui.Field(c, "移除环境变量（每行一个名称）", func() { ui.TextArea(c, &f.unset).Height(50).FillWidth() })
							ui.Checkbox(c, &f.input.Enabled, "自动运行")
							if f.input.Schedule != nil {
								ui.Checkbox(c, &f.input.Schedule.Enabled, "启用计划")
							}
						})
					})
				})
			})
			if f.error != "" {
				ui.Text(c, f.error).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, "取消").Disabled(f.busy).Clicked() {
					f.win.Close()
				}
				label := "保存"
				if f.busy {
					label = "保存中…"
				}
				if ui.PrimaryButton(c, label).Disabled(f.busy).Clicked() {
					in := f.input
					in.EnvSet = map[string]string{}
					in.EnvUnset = nil
					f.error = ""
					for _, line := range strings.Split(f.env, "\n") {
						if line == "" {
							continue
						}
						k, v, ok := strings.Cut(line, "=")
						if !ok || k == "" {
							f.error = "环境变量格式应为 KEY=VALUE"
							break
						}
						in.EnvSet[k] = v
					}
					for _, line := range strings.Split(f.unset, "\n") {
						if line != "" {
							in.EnvUnset = append(in.EnvUnset, line)
						}
					}
					duration, e := time.ParseDuration(f.timeout)
					if e != nil || duration <= 0 {
						f.error = "超时应为正时长，例如 30m"
					}
					in.TimeoutMS = duration.Milliseconds()
					in.Executable = f.command
					in.Args = []string{}
					if f.mode == 1 {
						in.Executable = "/bin/zsh"
						in.Args = []string{"-lc", f.command}
					} else if f.args != "" {
						in.Args = strings.Split(f.args, "\n")
					}
					in.Schedule = nil
					if f.plan > 0 {
						typ := "cron"
						if f.plan == 2 {
							typ = "interval"
						}
						in.Schedule = &model.Schedule{Type: typ, Expression: f.expression, Timezone: f.zone, Enabled: true}
						if f.input.Schedule != nil {
							in.Schedule.ID = f.input.Schedule.ID
							in.Schedule.Enabled = f.input.Schedule.Enabled
						}
					}
					if f.error == "" {
						f.busy = true
						go func() {
							_, err := s.service.SaveTask(f.id, in, f.revision)
							f.win.Update(func() {
								f.busy = false
								if err != nil {
									f.error = err.Error()
								} else {
									f.win.Close()
									go s.refresh()
								}
							})
						}()
					}
				}
			})
		})
	})})
	return f
}
func (s *windowState) showSettings() {
	if s.settingsWin != nil && !s.settingsWin.IsDestroyed() {
		s.settingsWin.Show()
		return
	}
	loginSupported := supportsLoginItem()
	loginEnabled := false
	if loginSupported {
		loginEnabled = mygo.App.OpenAtLogin()
	}
	v := s.service.Settings()
	timeout := time.Duration(v.DefaultTimeoutMS * int64(time.Millisecond)).String()
	max := strconv.Itoa(v.MaxConcurrent)
	days := strconv.Itoa(v.RetentionDays)
	limit := strconv.FormatInt(v.LogLimitBytes/(1024*1024), 10)
	message := ""
	busy := false
	s.settingsWin = mygo.NewWindow(mygo.WindowOptions{Title: "设置", Width: 480, Height: 460, MinWidth: 420, MinHeight: 340, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					ui.Field(c, "默认超时", func() { ui.TextInput(c, &timeout).FillWidth() })
					ui.Field(c, "同时运行任务数", func() { ui.TextInput(c, &max).FillWidth() })
					ui.Field(c, "历史保留天数", func() { ui.TextInput(c, &days).FillWidth() })
					ui.Field(c, "单个日志上限（MiB）", func() { ui.TextInput(c, &limit).FillWidth() })
					ui.Checkbox(c, &v.NotifyFailure, "任务失败时通知")
					ui.Checkbox(c, &v.NotifySuccess, "任务成功时通知")
					ui.Checkbox(c, &v.NotifyObserver, "观察状态变化时通知")
					if loginSupported {
						if ui.Checkbox(c, &loginEnabled, "登录时打开 apilot").Changed() {
							wanted := loginEnabled
							go func() {
								err := mygo.App.SetOpenAtLogin(wanted)
								s.settingsWin.Update(func() {
									if err != nil {
										message = err.Error()
										loginEnabled = mygo.App.OpenAtLogin()
									}
								})
							}()
						}
					}

				})
			})
			if message != "" {
				ui.Text(c, message)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, "保存").Disabled(busy).Clicked() {
					duration, e := time.ParseDuration(timeout)
					if e != nil {
						message = "默认超时格式无效"
						return
					}
					n, e := strconv.Atoi(max)
					if e != nil {
						message = "同时运行任务数应为整数"
						return
					}
					d, e := strconv.Atoi(days)
					if e != nil {
						message = "保留天数应为整数"
						return
					}
					l, e := strconv.ParseInt(limit, 10, 64)
					if e != nil {
						message = "日志上限应为整数"
						return
					}
					copy := v
					copy.DefaultTimeoutMS = duration.Milliseconds()
					copy.MaxConcurrent = n
					copy.RetentionDays = d
					copy.LogLimitBytes = l * 1024 * 1024
					busy = true
					go func() {
						saved, e := s.service.SaveSettings(copy)
						s.settingsWin.Update(func() {
							busy = false
							if e != nil {
								message = e.Error()
							} else {
								v = saved
								message = "已保存"
							}
						})
					}()
				}
			})
		})
	})})
}
