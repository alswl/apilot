package workbench

import (
	"runtime"
	"time"

	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// HostedWorkspace is the production workbench presented by the native App lifecycle.
type HostedWorkspace struct{ model *mockup }

const builtinPreferenceTabs = 4

const workbenchMinWidth, workbenchMinFrameHeight = 680, 580

const CLISettingsTab = builtinPreferenceTabs + 1

type HostPanels struct {
	Automation        func(*ui.Context)
	RunningAutomation func() int
	PreferencePanels  []PreferencePanel
}

type PreferencePanel struct {
	Title func() string
	View  func(*ui.Context, *mygo.Window)
}

func NewHostedWorkspace(service *services.Remote, keybindingsPath, language string, panels HostPanels) (*HostedWorkspace, error) {
	if _, err := resolveLanguage(language, "en"); err != nil {
		return nil, err
	}
	m := newFramework()
	m.connectionsData = nil
	m.live = service
	m.hosted = true
	m.fixtureConnector = localConnectorFixture()
	m.hostPanels = panels
	m.useAdaptiveLayout()
	m.loadHostedNavigation(config.DataDir())
	m.keyboard.path = keybindingsPath
	m.keyboard.reload()
	m.initLanguage(language)
	return &HostedWorkspace{m}, nil
}

func workbenchMinimumHeight() int {
	if runtime.GOOS == "darwin" {
		// The hidden-inset toolbar occupies 34 pt at the AppKit resize boundary.
		return workbenchMinFrameHeight - 34
	}
	return workbenchMinFrameHeight
}

func (w *HostedWorkspace) Open() *mygo.Window {
	m := w.model
	if m.win == nil || m.win.IsDestroyed() {
		m.win = mygo.NewWindow(mygo.WindowOptions{
			Title: m.windowTitle(), Width: 1190, Height: 730, MinWidth: workbenchMinWidth, MinHeight: workbenchMinimumHeight(), StateKey: "workbench",
			TitleBarStyle: mygo.TitleBarHiddenInset, Content: ui.View(m.view),
		})
		m.win.OnBlur(func() { m.keyboard.prefix = keystroke{} })
		m.win.OnFocus(func() {
			if result := m.customQuery.result; result != nil {
				if zone, err := time.LoadLocation(m.customQuery.config.Timezone); err == nil && result.AsOfDate != time.Now().In(zone).Format(time.DateOnly) {
					m.invalidateTaskQuery()
					m.win.Invalidate()
				}
			}
		})
		m.startLiveEvents()
	}
	m.win.Show()
	m.win.Focus()
	return m.win
}

func (w *HostedWorkspace) ShowAutomation() {
	w.model.navigate(scheduledPage)
	if win := w.model.win; win != nil && !win.IsDestroyed() {
		win.Invalidate()
	}
}

func (w *HostedWorkspace) EffectiveLocale() string {
	if englishUI.Load() {
		return "en"
	}
	return "zh-Hans"
}

func (w *HostedWorkspace) InstallMenu() { w.model.setMenu() }

func (w *HostedWorkspace) UpdateLocale(locale string) {
	english, _ := resolveLanguage("", locale)
	englishUI.Store(english)
	w.model.languageChoice = mygo.App.ChosenLocale()
	w.model.invalidateWindows()
}

func (w *HostedWorkspace) Settings(tab int) *mygo.Window {
	if tab >= 0 {
		w.model.settingsTab = tab
	}
	w.model.openSettings()
	w.model.preferences.Invalidate()
	return w.model.preferences
}
