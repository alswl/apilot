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
		return tr("manualRun")
	}
	label := t.Schedule.Expression
	if t.Schedule.Type == "interval" {
		label = tr("every") + label
	}
	if !t.Enabled || !t.Schedule.Enabled {
		return label + tr("automaticRunsOff")
	}
	if t.Schedule.NextRunAt != nil {
		label += tr("next") + t.Schedule.NextRunAt.Local().Format("01-02 15:04:05")
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
	title := tr("newTask")
	if t != nil {
		title = tr("editTask")
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
					ui.Field(c, tr("name"), func() { ui.TextInput(c, &f.input.Name).FillWidth() })
					if ui.Tabs(c, &f.mode, tr("command"), "Shell").Changed() {
						f.command = ""
						f.args = ""
					}
					label := tr("executable")
					if f.mode == 1 {
						label = tr("shellCommand")
					}
					ui.Field(c, label, func() {
						if f.mode == 1 {
							ui.TextArea(c, &f.command).Height(70).FillWidth()
						} else {
							ui.TextInput(c, &f.command).Placeholder("/bin/echo").FillWidth()
						}
					})
					if f.mode == 0 {
						ui.Field(c, tr("argumentsOnePerLine"), func() { ui.TextArea(c, &f.args).Height(60).FillWidth() })
					}
					ui.Field(c, tr("workingDirectory"), func() { ui.TextInput(c, &f.input.Cwd).FillWidth() })
					ui.Tabs(c, &f.plan, tr("manual"), tr("cron"), tr("interval"))
					if f.plan > 0 {
						label := tr("cronExpression")
						example := "0 9 * * 1-5"
						if f.plan == 2 {
							label = tr("runInterval")
							example = "30m"
						}
						ui.Field(c, label, func() { ui.TextInput(c, &f.expression).Placeholder(example).FillWidth() })
					}
					ui.Collapsible(c, tr("advanced"), &f.advanced, func() {
						ui.Column(c).FillWidth().Gap(10).Children(func() {
							ui.Field(c, tr("description"), func() { ui.TextInput(c, &f.input.Description).FillWidth() })
							ui.Field(c, tr("timeout"), func() { ui.TextInput(c, &f.timeout).FillWidth() })
							if f.plan == 1 {
								ui.Field(c, tr("timeZone"), func() { ui.TextInput(c, &f.zone).FillWidth() })
							}
							ui.Field(c, tr("addEnvironmentVariablesKeyValuePerLine"), func() { ui.TextArea(c, &f.env).Height(60).FillWidth() })
							ui.Field(c, tr("removeEnvironmentVariablesOneNamePerLine"), func() { ui.TextArea(c, &f.unset).Height(50).FillWidth() })
							ui.Checkbox(c, &f.input.Enabled, tr("automaticRuns"))
							if f.input.Schedule != nil {
								ui.Checkbox(c, &f.input.Schedule.Enabled, tr("enableSchedule"))
							}
						})
					})
				})
			})
			if f.error != "" {
				ui.Text(c, f.error).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, tr("cancel")).Disabled(f.busy).Clicked() {
					f.win.Close()
				}
				label := tr("save")
				if f.busy {
					label = tr("saving")
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
							f.error = tr("environmentVariablesMustUseKeyValue")
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
						f.error = tr("timeoutMustBePositiveForExample30M")
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
	language := mygo.App.ChosenLocale()
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
	s.settingsWin = mygo.NewWindow(mygo.WindowOptions{Title: tr("settings"), Width: 480, Height: 460, MinWidth: 420, MinHeight: 340, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					languagePicker(c, &language, mygo.App.SetLocale)
					ui.Field(c, tr("defaultTimeout"), func() { ui.TextInput(c, &timeout).FillWidth() })
					ui.Field(c, tr("concurrentTasks"), func() { ui.TextInput(c, &max).FillWidth() })
					ui.Field(c, tr("historyRetentionDays"), func() { ui.TextInput(c, &days).FillWidth() })
					ui.Field(c, tr("logLimitPerStreamMib"), func() { ui.TextInput(c, &limit).FillWidth() })
					ui.Checkbox(c, &v.NotifyFailure, tr("notifyOnTaskFailure"))
					ui.Checkbox(c, &v.NotifySuccess, tr("notifyOnTaskSuccess"))
					ui.Checkbox(c, &v.NotifyObserver, tr("notifyOnObserverStateChanges"))
					if ui.Button(c, tr("commandLineTool")).Clicked() {
						s.showCLI()
					}
					if loginSupported {
						if ui.Checkbox(c, &loginEnabled, tr("openApilotAtLogin")).Changed() {
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
				if ui.PrimaryButton(c, tr("save")).Disabled(busy).Clicked() {
					duration, e := time.ParseDuration(timeout)
					if e != nil {
						message = tr("invalidDefaultTimeout")
						return
					}
					n, e := strconv.Atoi(max)
					if e != nil {
						message = tr("concurrentTasksMustBeAnInteger")
						return
					}
					d, e := strconv.Atoi(days)
					if e != nil {
						message = tr("retentionDaysMustBeAnInteger")
						return
					}
					l, e := strconv.ParseInt(limit, 10, 64)
					if e != nil {
						message = tr("logLimitMustBeAnInteger")
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
								message = tr("saved")
							}
						})
					}()
				}
			})
		})
	})})
}
