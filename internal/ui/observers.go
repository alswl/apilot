package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/alswl/apilot/internal/presentation"
	"github.com/alswl/apilot/pkg/common"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (s *windowState) observerList(c *ui.Context) {
	if s.selectedObserver != "" {
		s.observerDetail(c)
		return
	}
	ui.Row(c).FillWidth().Children(func() {
		ui.Spacer(c).Grow(1)
		if ui.Button(c, "＋").Label(tr("newObserver")).Tooltip(tr("newObserver")).Clicked() {
			s.editObserver(nil)
		}
	})
	if len(s.observers) == 0 {
		ui.Column(c).Fill().Center().Gap(12).Children(func() {
			ui.Text(c, tr("noObservers"))
			if ui.PrimaryButton(c, tr("newObserver")).Clicked() {
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
					label += tr("disabled")
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
		return tr("healthy")
	case "unhealthy":
		return tr("unhealthy")
	default:
		return tr("notCheckedYet")
	}
}
func (s *windowState) observerDetail(c *ui.Context) {
	var observer *common.Observer
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
		if ui.Button(c, "‹").Label(tr("backToObservers")).Tooltip(tr("backToObservers")).Clicked() {
			s.selectedObserver = ""
		}
		ui.Text(c, v.Name).Grow(1).Bold()
		if ui.Button(c, tr("edit")).Clicked() {
			s.editObserver(&v)
		}
	})
	ui.Text(c, healthText(v.Health))
	if len(v.EnvKeys) > 0 {
		ui.Text(c, tr("environmentVariables")+strings.Join(v.EnvKeys, ", "))
	}
	on := v.Enabled
	if ui.Checkbox(c, &on, tr("automaticChecks")).Changed() {
		s.action(func() error { _, e := s.service.SetObserverEnabled(v.ID, on); return e })
	}
	if v.LastCheckedAt != nil {
		ui.Text(c, tr("lastChecked")+v.LastCheckedAt.Local().Format("01-02 15:04:05")).FontSize(12)
	}
	if v.Error != "" {
		ui.Text(c, v.Error).TextColor(c.Theme().Danger)
	}
	text := v.Result
	ui.TextArea(c, &text).ReadOnly(true).Grow(1).Label(tr("latestObservation"))
	for _, event := range s.events {
		if event.SourceType == "observer" && event.SourceID == v.ID && event.AcknowledgedAt == nil {
			event := event
			if ui.Button(c, tr("markAsRead")).Clicked() {
				s.action(func() error { _, e := s.service.AckEvent(event.ID); return e })
			}
			break
		}
	}
	if ui.Button(c, tr("deleteObserver2")).Clicked() {
		s.background(func() {
			result, e := mygo.Dialog.Message(mygo.MessageOptions{Parent: s.win, Type: mygo.MessageWarning, Message: fmt.Sprintf(tr("deleteObserverS"), v.Name), Detail: tr("stopFutureChecksTheObservedToolIsLeftUnchanged"), Buttons: []string{tr("cancel"), tr("deleteObserver")}})
			if e == nil && result.Button == 1 {
				s.update(func() { s.action(func() error { return s.service.DeleteObserver(v.ID) }) })
			}
		})
	}
}
func (s *windowState) editObserver(v *common.Observer) {
	home, _ := os.UserHomeDir()
	draft := presentation.NewObserverDraft(v, home)
	title := tr("newObserver")
	if v != nil {
		title = tr("editObserver")
	}
	var operation presentation.OperationState
	message := ""
	busy := false
	advanced := false
	var win *mygo.Window
	win = mygo.NewWindow(mygo.WindowOptions{Title: title, Width: 540, Height: 500, MinWidth: 480, MinHeight: 360, Parent: s.win, Modal: true, Content: ui.View(func(c *ui.Context) {
		brandTheme(c)
		ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
			ui.Text(c, title).Bold().FontSize(17)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					ui.Field(c, tr("name"), func() { ui.TextInput(c, &draft.Values.Name).FillWidth() })
					ui.Field(c, tr("executable"), func() { ui.TextInput(c, &draft.Values.Executable).FillWidth() })
					ui.Field(c, tr("argumentsOnePerLine"), func() { ui.TextArea(c, &draft.Args).Height(60).FillWidth() })
					ui.Field(c, tr("workingDirectory"), func() { ui.TextInput(c, &draft.Values.Cwd).FillWidth() })
					ui.Field(c, tr("checkInterval"), func() { ui.TextInput(c, &draft.Interval).FillWidth() })
					ui.Collapsible(c, tr("advanced"), &advanced, func() {
						ui.Field(c, tr("timeout"), func() { ui.TextInput(c, &draft.Timeout).FillWidth() })
						ui.Tabs(c, &draft.Format, tr("text"), "JSON")
						ui.Checkbox(c, &draft.Values.Enabled, tr("automaticChecks"))
						ui.Field(c, tr("addEnvironmentVariablesKeyValuePerLine"), func() { ui.TextArea(c, &draft.Env).Height(60).FillWidth() })
						ui.Field(c, tr("removeEnvironmentVariablesOneNamePerLine"), func() { ui.TextArea(c, &draft.Unset).Height(50).FillWidth() })
					})
				})
			})
			if message != "" {
				ui.Text(c, message).TextColor(c.Theme().Danger)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, tr("cancel")).Disabled(busy).Clicked() {
					win.Close()
				}
				if ui.PrimaryButton(c, tr("save")).Disabled(busy).Clicked() {
					copy, fieldErr := draft.Input()
					if fieldErr != nil {
						message = presentationMessage(fieldErr.Code)
						return
					}
					token, ok := operation.Begin()
					if !ok {
						return
					}
					ref, revision := draft.ID, draft.Revision

					busy = true
					s.background(func() {
						_, e := s.service.SaveObserver(ref, copy, revision)
						win.Update(func() {
							if s.stopping || !operation.Complete(token) {
								return
							}
							busy = false
							if e != nil {
								message = e.Error()
							} else {
								win.Close()
								s.refresh()
							}
						})
					})
				}
			})
		})
	})})

	win.OnClosed(func() { operation.Invalidate() })
}
