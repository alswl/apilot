package ui

import (
	"github.com/alswl/apilot/internal/presentation"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func (s *windowState) showSettings() {
	if s.workspace != nil {
		s.settingsWin = s.workspace.Settings(-1)
	}
}

func (s *windowState) cliPreferences(c *ui.Context, w *mygo.Window) {
	if s.cliPanelWindow != w || s.cliPanel == nil {
		s.cliPanelWindow = w
		s.cliPanel = newCLISettings()
	}
	s.cliPanel.view(c)
}

func (s *windowState) automationPreferences() func(*ui.Context, *mygo.Window) {
	var lastWindow *mygo.Window
	var draft presentation.SettingsDraft
	var operation presentation.OperationState
	var loginSupported, loginEnabled, busy bool
	var message string
	return func(c *ui.Context, w *mygo.Window) {
		if lastWindow != w {
			lastWindow = w
			draft = presentation.NewSettingsDraft(s.settings)
			loginSupported = supportsLoginItem()
			loginEnabled = loginSupported && mygo.App.OpenAtLogin()
			busy, message = false, ""
			operation.Invalidate()
			w.OnClose(func(e *mygo.CloseEvent) {
				if busy && !s.stopping {
					e.PreventDefault()
				}
			})
			w.OnClosed(func() { operation.Invalidate() })
		}
		ui.Scroll(c.Key("values")).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				ui.Field(c, tr("defaultTimeout"), func() { ui.TextInput(c, &draft.Timeout).FillWidth() })
				ui.Field(c, tr("concurrentTasks"), func() { ui.TextInput(c, &draft.Max).FillWidth() })
				ui.Field(c, tr("historyRetentionDays"), func() { ui.TextInput(c, &draft.Days).FillWidth() })
				ui.Field(c, tr("logLimitPerStreamMib"), func() { ui.TextInput(c, &draft.Limit).FillWidth() })
				ui.Checkbox(c, &draft.Values.NotifyFailure, tr("notifyOnTaskFailure"))
				ui.Checkbox(c, &draft.Values.NotifySuccess, tr("notifyOnTaskSuccess"))
				ui.Checkbox(c, &draft.Values.NotifyObserver, tr("notifyOnObserverStateChanges"))
				if loginSupported && ui.Checkbox(c, &loginEnabled, tr("openApilotAtLogin")).Changed() {
					wanted := loginEnabled
					s.background(func() {
						err := mygo.App.SetOpenAtLogin(wanted)
						w.Update(func() {
							if s.stopping || w.IsDestroyed() {
								return
							}
							if err != nil {
								message = err.Error()
								loginEnabled = mygo.App.OpenAtLogin()
							}
						})
					})
				}
			})
		})
		if message != "" {
			ui.Text(c, message)
		}
		ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
			if !ui.PrimaryButton(c, tr("save")).Disabled(busy).Clicked() {
				return
			}
			values, fieldErr := draft.Settings()
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
				saved, err := s.service.SaveSettings(values)
				w.Update(func() {
					if s.stopping || w.IsDestroyed() || !operation.Complete(token) {
						return
					}
					busy = false
					if err != nil {
						message = err.Error()
						return
					}
					s.settings = saved
					draft = presentation.NewSettingsDraft(saved)
					message = tr("saved")
				})
			})
		})
	}
}
