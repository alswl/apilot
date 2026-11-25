package workbench

import (
	"slices"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var languageTags = [...]string{"", "zh-Hans", "en"}

func languageIndex(tag string) int {
	return max(0, slices.Index(languageTags[:], tag))
}

func (m *mockup) initLanguage(launchLanguage string) {
	m.languageChoice = languageTags[languageIndex(mygo.App.ChosenLocale())]
	shown := mygo.App.Locale()
	if launchLanguage != "" {
		m.languageChoice = languageTags[languageIndex(launchLanguage)]
		if launchLanguage == "system" {
			shown = closestSystemLanguage()
		}
	}
	useEnglish, _ := resolveLanguage(launchLanguage, shown)
	englishUI.Store(useEnglish)
	m.applyLocale = func(tag string) {
		var useEnglish bool
		if m.framework && !m.hosted {
			useEnglish, _ = resolveLanguage(tag, closestSystemLanguage())
		} else {
			mygo.App.SetLocale(tag)
			useEnglish, _ = resolveLanguage("", mygo.App.Locale())
		}
		englishUI.Store(useEnglish)
		m.setMenu()
		m.invalidateWindows()
	}
}

func (m *mockup) invalidateWindows() {
	if m.win != nil && !m.win.IsDestroyed() {
		m.win.SetTitle(m.windowTitle())
		m.win.Invalidate()
	}
	if m.preferences != nil && !m.preferences.IsDestroyed() {
		m.preferences.SetTitle(tr("Apilot 设置"))
		m.preferences.Invalidate()
	}
}

func closestSystemLanguage() string {
	for _, tag := range mygo.App.PreferredLanguages() {
		if tag == "en" || tag == "zh" || strings.HasPrefix(tag, "en-") || strings.HasPrefix(tag, "zh-") {
			return tag
		}
	}
	return "en"
}

func (m *mockup) openSettings() {
	if m.preferences != nil && !m.preferences.IsDestroyed() {
		m.preferences.Show()
		m.preferences.Focus()
		return
	}
	m.preferences = mygo.NewWindow(mygo.WindowOptions{
		Title: tr("Apilot 设置"), Width: 620, Height: 480, MinWidth: 520, MinHeight: 420,
		Content: ui.View(func(c *ui.Context) {
			components.ApplyTheme(c)
			ui.Column(c).Fill().Padding(16).Gap(16).Background(c.Theme().Surface).Children(func() { m.settings(c) })
		}),
	})
	m.preferences.Show()
}

func (m *mockup) settings(c *ui.Context) {
	tabs := []string{tr("常规"), tr("助理"), tr("扩展"), tr("快捷键")}
	for _, panel := range m.hostPanels.PreferencePanels {
		tabs = append(tabs, panel.Title())
	}
	ui.Tabs(c.Key("settings-tabs"), &m.settingsTab, tabs...).FillWidth()
	if m.settingsTab >= builtinPreferenceTabs {
		index := m.settingsTab - builtinPreferenceTabs
		if index < len(m.hostPanels.PreferencePanels) {
			m.hostPanels.PreferencePanels[index].View(c.Key(index), m.preferences)
		}
		return
	}
	ui.Scroll(c.Key(m.settingsTab)).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			switch m.settingsTab {
			case 0:
				ui.Field(c.Key("language"), tr("语言"), func() {
					labels := []string{tr("跟随系统"), "简体中文", "English"}
					selected := labels[languageIndex(m.languageChoice)]
					if ui.Select(c, &selected, labels).Label(tr("界面语言")).Width(220).Changed() {
						if index := slices.Index(labels, selected); index >= 0 {
							m.languageChoice = languageTags[index]
							if m.applyLocale != nil {
								m.applyLocale(m.languageChoice)
							}
						}
					}
				})
			case 1:
				m.changeTag(c, "provider-settings", tr("推理配置与助理使用的资料集中管理。"))
				if m.framework {
					components.Muted(c, tr("助理尚未接入"))
				} else {
					ui.Field(c.Key("default-provider"), tr("默认模型"), func() {
						localizedSelect(c, &m.provider, []string{"工作模型", "个人模型"}, 0)
					})
					components.Muted(c, tr("演示配置 · 无实际凭据"))
				}
				ui.Divider(c)
				for _, name := range []string{"助手库", "知识库", "写作库", "记忆"} {
					ui.Row(c.Key(name)).FillWidth().Gap(16).Children(func() {
						components.DataText(c, tr(name))
						ui.Spacer(c).Grow(1)
						components.Muted(c, tr("未配置"))
					})
				}
			case 2:
				if m.live != nil {
					m.livePluginSettings(c)
					return
				}
				for _, conn := range m.connectionsData {
					ui.Row(c.Key(conn.ID)).FillWidth().Gap(12).Children(func() {
						components.DataText(c, tr(conn.ConnectorID))
						ui.Spacer(c).Grow(1)
						if m.framework {
							components.Muted(c, tr("未接入"))
						} else {
							components.Muted(c, tr("演示连接"))
						}
					})
				}
			case 3:
				m.keyboardSettings(c)
			}
		})
	})
}

func (m *mockup) keyboardSettings(c *ui.Context) {
	if m.keyboard.path == "" {
		components.Muted(c, tr("内置键位 · 用 --keybindings 指定配置文件"))
	} else {
		components.Muted(c, m.keyboard.path)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c.Key("open-bindings"), tr("打开配置文件")).Clicked() {
				if err := mygo.Shell.OpenPath(m.keyboard.path); err != nil {
					m.keyboard.loadError = err.Error()
				}
			}
			if ui.Button(c.Key("reload-bindings"), tr("重新加载")).Clicked() {
				m.keyboard.reload()
				if m.keyboard.loadError == "" {
					m.setMenu()
				}
				if m.win != nil {
					m.win.Invalidate()
				}
			}
		})
	}
	if m.keyboard.loadError != "" {
		components.Muted(c, m.keyboard.loadError)
	}
}

func (m *mockup) livePluginSettings(c *ui.Context) {
	for _, plugin := range m.livePlugins {
		if !m.visibleConnectorKind(plugin.ID) {
			continue
		}
		title := plugin.ID
		for _, connector := range services.WorkbenchConnectors() {
			if connector.Kind == plugin.Connector {
				title = connector.Name
				break
			}
		}
		if m.connectorDesktop() {
			title = connectorLabel(plugin.ID)
		}
		if plugin.Release != nil {
			title += " · " + plugin.Release.ReleaseVersion
		}
		enabled := plugin.Enabled
		if ui.Checkbox(c.Key(plugin.ID), &enabled, title).Changed() {
			if err := m.live.SetWorkbenchPluginEnabled(plugin.ID, enabled); err != nil {
				m.livePluginProblem = common.ProtocolError(err).Message
			} else {
				m.livePluginProblem = ""
				m.liveGeneration++
				m.refreshLive()
			}
		}
	}
	if m.livePluginProblem != "" {
		components.Muted(c.Key("plugin-problem"), m.livePluginProblem)
	}
}
