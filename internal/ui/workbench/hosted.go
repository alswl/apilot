package workbench

import (
	"log"
	"path/filepath"

	"github.com/alswl/apilot/pkg/config"
	"github.com/alswl/apilot/pkg/services"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// HostedWorkspace is the production workbench presented by the native App lifecycle.
type HostedWorkspace struct{ model *mockup }

const builtinPreferenceTabs = 4

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
	if err := m.loadTaskViewStore(filepath.Join(config.DataDir(), "task-views.json")); err != nil {
		log.Printf("Could not load saved task views: %v", err)
	}
	m.hostPanels = panels
	m.useAdaptiveLayout()
	m.keyboard.path = keybindingsPath
	m.keyboard.reload()
	m.initLanguage(language)
	return &HostedWorkspace{m}, nil
}

func (w *HostedWorkspace) Open() *mygo.Window {
	m := w.model
	if m.win == nil || m.win.IsDestroyed() {
		m.win = mygo.NewWindow(mygo.WindowOptions{
			Title: m.windowTitle(), Width: 1190, Height: 730, MinWidth: 680, MinHeight: 580, StateKey: "workbench",
			TitleBarStyle: mygo.TitleBarHiddenInset, Content: ui.View(m.view),
		})
		m.win.OnBlur(func() { m.keyboard.prefix = keystroke{} })
		m.pollLive()
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
