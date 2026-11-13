package ui

import (
	"os"
	"strings"

	"github.com/alswl/apilot/internal/presentation"
	"github.com/alswl/apilot/pkg/common"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func contains(s, q string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(q)) }
func planLabel(t common.Task) string {
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
	win *mygo.Window
	presentation.TaskDraft
	error          string
	advanced, busy bool
	operation      presentation.OperationState
}

func (s *windowState) editTask(t *common.Task) *taskForm {
	if s.focusEditor() {
		return s.taskEditor
	}
	s.openTasks()
	if t != nil {
		s.selectedTask = t.ID
	}
	home, _ := os.UserHomeDir()
	f := &taskForm{TaskDraft: presentation.NewTaskDraft(t, s.service.Settings(), home)}
	title := tr("newTask")
	if t != nil {
		title = tr("editTask")
	}
	f.win = mygo.NewWindow(mygo.WindowOptions{Title: title, Width: 560, Height: 520, MinWidth: 480, MinHeight: 360, Parent: s.win, Modal: true, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Text(c, title).Bold().FontSize(17)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					ui.Field(c, tr("name"), func() { ui.TextInput(c, &f.Values.Name).FillWidth() })
					if ui.Tabs(c, &f.Mode, tr("command"), "Shell").Changed() {
						f.Command = ""
						f.Args = ""
					}
					label := tr("executable")
					if f.Mode == 1 {
						label = tr("shellCommand")
					}
					ui.Field(c, label, func() {
						if f.Mode == 1 {
							ui.TextArea(c, &f.Command).Height(70).FillWidth()
						} else {
							ui.TextInput(c, &f.Command).Placeholder("/bin/echo").FillWidth()
						}
					})
					if f.Mode == 0 {
						ui.Field(c, tr("argumentsOnePerLine"), func() { ui.TextArea(c, &f.Args).Height(60).FillWidth() })
					}
					ui.Field(c, tr("workingDirectory"), func() { ui.TextInput(c, &f.Values.Cwd).FillWidth() })
					ui.Tabs(c, &f.Plan, tr("manual"), tr("cron"), tr("interval"))
					if f.Plan > 0 {
						label := tr("cronExpression")
						example := "0 9 * * 1-5"
						if f.Plan == 2 {
							label = tr("runInterval")
							example = "30m"
						}
						ui.Field(c, label, func() { ui.TextInput(c, &f.Expression).Placeholder(example).FillWidth() })
					}
					ui.Collapsible(c, tr("advanced"), &f.advanced, func() {
						ui.Column(c).FillWidth().Gap(10).Children(func() {
							ui.Field(c, tr("description"), func() { ui.TextInput(c, &f.Values.Description).FillWidth() })
							ui.Field(c, tr("timeout"), func() { ui.TextInput(c, &f.Timeout).FillWidth() })
							if f.Plan == 1 {
								ui.Field(c, tr("timeZone"), func() { ui.TextInput(c, &f.Zone).FillWidth() })
							}
							ui.Field(c, tr("addEnvironmentVariablesKeyValuePerLine"), func() { ui.TextArea(c, &f.Env).Height(60).FillWidth() })
							ui.Field(c, tr("removeEnvironmentVariablesOneNamePerLine"), func() { ui.TextArea(c, &f.Unset).Height(50).FillWidth() })
							ui.Checkbox(c, &f.Values.Enabled, tr("automaticRuns"))
							if f.Values.Schedule != nil {
								ui.Checkbox(c, &f.Values.Schedule.Enabled, tr("enableSchedule"))
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
					in, fieldErr := f.Input()
					f.error = ""
					if fieldErr != nil {
						f.error = presentationMessage(fieldErr.Code)
					}

					if f.error == "" {
						token, ok := f.operation.Begin()
						if !ok {
							return
						}
						f.busy = true
						id, revision := f.ID, f.Revision
						s.background(func() {
							saved, err := s.service.SaveTask(id, in, revision)
							f.win.Update(func() {
								if s.stopping || !f.operation.Complete(token) {
									return
								}
								f.busy = false
								if err != nil {
									f.error = err.Error()
								} else {
									found := false
									for i := range s.tasks {
										if s.tasks[i].ID == saved.ID {
											s.tasks[i] = saved
											found = true
											break
										}
									}
									if !found {
										s.tasks = append(s.tasks, saved)
									}
									s.selectedTask = saved.ID
									f.win.Close()
									s.refresh()
								}
							})
						})
					}
				}
			})
		})
	})})
	s.taskEditor = f
	f.win.OnClose(func(e *mygo.CloseEvent) {
		if f.busy && !s.stopping {
			e.PreventDefault()
		}
	})
	f.win.OnClosed(func() {
		f.operation.Invalidate()
		s.taskEditor = nil
		if !s.stopping && !s.win.IsDestroyed() {
			s.win.Focus()
		}
	})
	return f
}
func (s *windowState) showSettings() {
	if s.settingsWin != nil && !s.settingsWin.IsDestroyed() {
		s.settingsWin.Show()
		s.settingsWin.Focus()
		s.settingsWin.Invalidate()
		return
	}
	language := mygo.App.ChosenLocale()
	loginSupported := supportsLoginItem()
	loginEnabled := false
	if loginSupported {
		loginEnabled = mygo.App.OpenAtLogin()
	}
	draft := presentation.NewSettingsDraft(s.service.Settings())
	cli := newCLISettings()
	var operation presentation.OperationState
	message := ""
	busy := false
	var win *mygo.Window
	win = mygo.NewWindow(mygo.WindowOptions{Title: tr("settings"), Width: 480, Height: 460, MinWidth: 420, MinHeight: 360, StateKey: "settings", Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Tabs(c, &s.settingsPage, tr("generalSettings"), tr("commandLineSettings"))
			if s.settingsPage == 1 {
				cli.view(c)
				return
			}
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					languagePicker(c, &language, mygo.App.SetLocale)
					ui.Field(c, tr("defaultTimeout"), func() { ui.TextInput(c, &draft.Timeout).FillWidth() })
					ui.Field(c, tr("concurrentTasks"), func() { ui.TextInput(c, &draft.Max).FillWidth() })
					ui.Field(c, tr("historyRetentionDays"), func() { ui.TextInput(c, &draft.Days).FillWidth() })
					ui.Field(c, tr("logLimitPerStreamMib"), func() { ui.TextInput(c, &draft.Limit).FillWidth() })
					ui.Checkbox(c, &draft.Values.NotifyFailure, tr("notifyOnTaskFailure"))
					ui.Checkbox(c, &draft.Values.NotifySuccess, tr("notifyOnTaskSuccess"))
					ui.Checkbox(c, &draft.Values.NotifyObserver, tr("notifyOnObserverStateChanges"))
					if loginSupported {
						if ui.Checkbox(c, &loginEnabled, tr("openApilotAtLogin")).Changed() {
							wanted := loginEnabled
							s.background(func() {
								err := mygo.App.SetOpenAtLogin(wanted)
								win.Update(func() {
									if s.stopping || win.IsDestroyed() {
										return
									}
									if err != nil {
										message = err.Error()
										loginEnabled = mygo.App.OpenAtLogin()
									}
								})
							})
						}
					}

				})
			})
			if message != "" {
				ui.Text(c, message)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, tr("save")).Disabled(busy).Clicked() {
					copy, fieldErr := draft.Settings()
					if fieldErr != nil {
						message = presentationMessage(fieldErr.Code)
						return
					}
					token, ok := operation.Begin()
					if !ok {
						return
					}

					busy = true
					s.background(func() {
						saved, e := s.service.SaveSettings(copy)
						win.Update(func() {
							if s.stopping || win.IsDestroyed() {
								return
							}
							if !operation.Complete(token) {
								return
							}
							busy = false
							if e != nil {
								message = e.Error()
							} else {
								draft = presentation.NewSettingsDraft(saved)
								message = tr("saved")
							}
						})
					})
				}
			})
		})
	})})

	s.settingsWin = win
	win.OnClose(func(e *mygo.CloseEvent) {
		if busy && !s.stopping {
			e.PreventDefault()
		}
	})
	win.OnClosed(func() { operation.Invalidate() })
}
