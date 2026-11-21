package workbench

import (
	"cmp"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	nowPage = iota
	tasksPage
	connectionsPage
	scheduledPage
	usagePage
)

var pages = [...]string{"Now", "任务", "连接", "定时任务", "用量"}

type mockup struct {
	devTask                              *devTaskScenario
	hosted                               bool
	hostPanels                           HostPanels
	liveConnections                      []common.WorkbenchSnapshot
	livePlugins                          []services.WorkbenchPlugin
	liveTaskRequests                     map[string]common.WorkbenchTaskWrite
	liveTaskProblems                     map[string]string
	serverTaskOperations                 map[string]common.WorkbenchTaskReceipt
	checkingOperations                   map[string]bool
	livePluginProblem                    string
	syncDetail                           common.WorkbenchSnapshot
	syncDetailProblem                    string
	removeConnectionID                   string
	live                                 *services.Remote
	liveBusy, liveError, liveLoaded      bool
	liveGeneration                       uint64
	framework                            bool
	layout                               workbenchLayout
	taskPageIndex, taskPageSize          int
	taskPageSizeOverride                 int
	taskBackStack                        []taskReturnPoint
	returnRelatedPending                 bool
	taskPageCriteria                     taskPageCriteria
	sourceEditor                         *taskSourceEdit
	labelEditor                          *labelEdit
	pendingTaskEdits                     map[string]bool
	taskWatches                          map[string]bool
	taskWatchOnly                        bool
	fullTask, taskResourceOpen           bool
	taskResource                         taskResource
	taskView, taskGrouping, taskViewName string
	taskViewDrafts                       map[string]taskViewState
	savedTaskViews                       []savedTaskView
	taskViewStorePath                    string
	taskViewSaveError                    bool
	taskColumnsHidden                    map[string]bool
	taskGroupTables                      map[string]*ui.ListState
	taskDisplayOpen, saveTaskViewOpen    bool
	searchExpanded                       bool
	win, assistant, preferences          *mygo.Window
	keyboard                             keyboardState
	newTaskOpen, newTaskPending          bool
	newTaskDraft, newTaskSource          string
	newTaskTargets                       taskDestinations
	newTaskRequest                       common.WorkbenchTaskWrite
	page, selected                       int
	variant, state                       int
	writeGeneration                      uint64
	pendingSync                          map[string]bool
	syncPhase                            map[string]int
	syncAttempts                         map[string]int
	syncLogs                             map[string][]syncEvent
	connectorEditor                      *connectorEdit
	syncLogID                            string
	syncLogRestoreFocus                  string
	pageScroll                           [len(pages)]ui.ScrollState
	refundConfirmedAt                    string
	nowSessions                          []nowSession
	nowSessionIndex                      int
	nowSessionsHidden, nowLibraryOpen    bool
	nowQuery                             string
	briefDismissed                       bool
	debugOpen, showChangeTags            bool
	dueDraft                             string
	editingDue                           bool
	connectionsData                      []connection
	search                               string
	taskSelection                        string
	restoreTaskFocus                     bool
	connectionReturnToTasks              bool
	restoreConnectionEntryFocus          bool
	statusFilter, sourceFilter           string
	taskTable                            ui.ListState
	taskSort                             ui.SortOrder
	inspector                            bool
	assistantThreads                     map[string]*assistantThread
	taskDetails                          map[string]*taskDetailUI
	provider                             string
	objects                              []sourceObject
	feedback                             feedback
	mutation                             mutation
	assistantContext                     int
	assistantOpen                        bool
	assistantNeedsFocus                  bool
	assistantScroll                      ui.ScrollState
	settingsTab, usageTab                int
	languageChoice                       string
	applyLocale                          func(string)
	watching                             bool
	automation                           automationPreview
	gallery                              componentGallery
	closedTips                           map[string]bool
	hoveredTips                          map[string]bool
}

func newMockup() *mockup {
	m := newWorkspace(loadFixture())
	m.nowSessions = []nowSession{
		{title: "每日简报", briefing: true},
		{title: "发布检查", turns: []nowTurn{{prompt: "发布前还要核对什么？", reply: "先确认 GitHub 工单已更新，再核对 Obsidian 中的验收记录。", translated: true}}},
	}
	return m
}

func newFramework() *mockup {
	m := newWorkspace(scenarioFixture{})
	m.framework = true
	m.nowSessions = []nowSession{{title: "新建会话"}}
	m.seedConnectorScaffolds()
	return m
}

func newWorkspace(data scenarioFixture) *mockup {
	return &mockup{
		statusFilter: "全部状态", sourceFilter: "全部来源", provider: "工作模型", refundConfirmedAt: "今天 09:42",
		taskSort:          ui.SortOrder{Column: "title"},
		taskGrouping:      "不分组",
		taskPageSize:      defaultTaskPageSize,
		taskViewDrafts:    make(map[string]taskViewState),
		taskGroupTables:   make(map[string]*ui.ListState),
		taskWatches:       make(map[string]bool),
		taskColumnsHidden: map[string]bool{"priority": true, "labels": true, "assignee": true},
		automation:        newAutomationPreview(data.Automation, data.RunOutcomes),
		keyboard:          defaultKeyboard(),
		state:             2, dueDraft: data.Mutation.BaseFields["due"], connectionsData: data.Connections,
		objects: data.Objects, feedback: data.Feedback, mutation: data.Mutation,
		pendingSync:      make(map[string]bool),
		syncPhase:        make(map[string]int),
		syncAttempts:     make(map[string]int),
		syncLogs:         make(map[string][]syncEvent),
		pendingTaskEdits: make(map[string]bool),
		assistantThreads: make(map[string]*assistantThread),
		closedTips:       make(map[string]bool), hoveredTips: make(map[string]bool),
	}
}

func RunMock(capture, keybindingsPath, language string) error {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	return runWorkspace(m, capture, keybindingsPath, language)
}

func (m *mockup) windowTitle() string {
	if m.framework {
		return "Apilot"
	}
	return tr("Apilot · 模拟模式")
}

func runWorkspace(m *mockup, capture, keybindingsPath, language string) error {
	if _, err := resolveLanguage(language, "en"); err != nil {
		return err
	}
	if capture != "" {
		if err := os.MkdirAll(capture, 0700); err != nil {
			return err
		}
	}
	if capture == "" {
		m.useAdaptiveLayout()
	}
	m.keyboard.path = keybindingsPath
	m.keyboard.reload()
	done := make(chan error, 1)
	mygo.App.SetLanguages("en", "zh-Hans")
	mygo.App.OnReady(func() {
		m.initLanguage(language)
		mygo.Theme.SetSource(mygo.ThemeLight)
		m.setMenu()
		m.win = mygo.NewWindow(mygo.WindowOptions{
			Title: m.windowTitle(), Width: 1190, Height: 730, MinWidth: 680, MinHeight: 580,
			TitleBarStyle: mygo.TitleBarHiddenInset,
			Content:       ui.View(m.view),
		})
		m.win.OnClosed(func() { mygo.App.Quit() })
		m.win.OnBlur(func() { m.keyboard.prefix = keystroke{} })
		m.win.Show()
		m.pollLive()
		if capture != "" {
			go func() {
				done <- m.capture(capture)
				mygo.RunOnMain(func() { mygo.App.Quit() })
			}()
		}
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	if capture == "" {
		return nil
	}
	select {
	case err := <-done:
		return err
	default:
		return fmt.Errorf("capture interrupted")
	}
}

func (m *mockup) setMenu() {
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Apilot", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			{Label: tr("设置"), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { m.openSettings() }},
			mygo.Separator(), {Role: mygo.RoleQuit},
		}},
		{Role: mygo.RoleEditMenu},
		{Label: tr("命令"), Submenu: []*mygo.MenuItem{
			m.paletteMenuItem("command.palette", false),
			m.paletteMenuItem("shortcuts.help", true),
		}},
		{Label: tr("窗口"), Submenu: []*mygo.MenuItem{
			{Label: tr("问助理"), Click: func(*mygo.MenuItem, *mygo.Window) { m.openAssistant() }},
		}},
	}))
}

func (m *mockup) paletteMenuItem(id string, help bool) *mygo.MenuItem {
	label := tr(shortcutTitle(id))
	if keys := m.keyboard.display(id); keys != "" {
		label += "  " + keys
	}
	return &mygo.MenuItem{Label: label, Click: func(_ *mygo.MenuItem, window *mygo.Window) {
		if window != m.win {
			return
		}
		m.showPalette(help)
		m.win.Invalidate()
	}}
}

func (m *mockup) capture(dir string) error {
	for i, file := range []string{"workbench", "tasks", "connections", "scheduled-tasks", "settings", "assistant", "debug", "now-follow-up", "tasks-narrow", "now-narrow", "now-follow-up-narrow", "tasks-editing", "tasks-conflict", "tasks-unknown", "task-search", "tasks-full", "tasks-full-narrow", "tasks-grouped", "obsidian-edit", "github-edit", "github-scenario", "github-labels", "obsidian-labels", "now-after-acceptance", "component-gallery", "component-gallery-table", "component-gallery-writeback", "command-palette", "keyboard-help", "tasks-page-2", "layout-wide", "layout-narrow", "layout-sidebar-hidden", "layout-now", "layout-now-session", "layout-connections", "layout-connector-config", "layout-sync-log", "layout-usage", "layout-assistant", "layout-expanded-detail", "layout-now-library"} {
		var win *mygo.Window
		ready := make(chan struct{})
		mygo.RunOnMain(func() {
			defer close(ready)
			win = m.win
			m.sourceEditor, m.labelEditor = nil, nil
			m.assistantOpen = false
			m.connectorEditor, m.syncLogID = nil, ""
			if file == "obsidian-edit" || file == "github-edit" || file == "github-scenario" || strings.HasSuffix(file, "-labels") {
				m.navigate(tasksPage)
				m.closeInspector()
				m.search, m.statusFilter, m.sourceFilter = "", "全部状态", "全部来源"
				index := 0
				if file == "github-edit" || file == "github-labels" {
					index = 2
				}
				if file == "github-scenario" {
					index = slices.IndexFunc(m.objects, func(object sourceObject) bool { return object.ID == "DEMO-101" })
				}
				m.openFullTask(index)
				if strings.HasSuffix(file, "-labels") {
					m.openLabels(m.objects[index])
				} else if file != "github-scenario" {
					m.openSourceEdit(m.objects[index])
				}
				win.Invalidate()
			} else if file == "tasks-full" || file == "tasks-full-narrow" || file == "tasks-grouped" {
				m.navigate(tasksPage)
				m.closeInspector()
				m.search, m.searchExpanded = "", false
				m.pageScroll[tasksPage] = ui.ScrollState{}
				m.taskSelection = "refund"
				if file != "tasks-grouped" {
					m.openFullTask(0)
				} else {
					m.taskGrouping = "按来源分组"
				}
				if file == "tasks-full-narrow" {
					win.SetSize(860, 580)
				} else {
					win.SetSize(1190, 730)
				}
				win.Invalidate()
			} else if file == "task-search" {
				m.navigate(tasksPage)
				m.closeInspector()
				m.search, m.searchExpanded = "label:refund", true
				m.debugOpen, m.showChangeTags = false, false
				m.mutation.Status = "confirmed"
				win.SetSize(1190, 730)
				win.Invalidate()
			} else if file == "command-palette" || file == "keyboard-help" {
				if m.gallery.window != nil {
					m.gallery.window.Close()
				}
				m.win.Focus()
				m.showPalette(file == "keyboard-help")
				m.win.Invalidate()
			} else if strings.HasPrefix(file, "component-gallery") {
				switch file {
				case "component-gallery-table":
					m.gallery.tab = 1
				case "component-gallery-writeback":
					m.gallery.tab = 2
				}
				m.gallery.show()
				win = m.gallery.window
				win.Invalidate()
			} else if file == "now-narrow" || file == "now-follow-up-narrow" {
				m.page, m.inspector = nowPage, false
				m.nowSession().turns = nil
				if file == "now-follow-up-narrow" {
					m.nowSession().draft = tr("验收通过后，我还需要做什么？")
					m.sendNowTurn()
				} else {
					m.nowSession().scroll = ui.ScrollState{}
				}
				win.Invalidate()
			} else if strings.HasPrefix(file, "layout-") {
				m.useAdaptiveLayout()
				m.closeInspector()
				m.openFullTask(1)
				win.SetSize(1190, 730)
				switch file {
				case "layout-narrow", "layout-sidebar-hidden":
					win.SetSize(940, 700)
					m.layout.sidebarHidden = file == "layout-sidebar-hidden"
				case "layout-now":
					m.navigate(nowPage)
					m.nowSession().scroll = ui.ScrollState{}
				case "layout-now-session":
					m.nowSessionIndex = 1
					m.navigate(nowPage)
				case "layout-connections":
					m.navigate(connectionsPage)
				case "layout-connector-config":
					m.navigate(connectionsPage)
					m.editConnector(m.connectionsData[0])
				case "layout-sync-log":
					m.navigate(connectionsPage)
					m.syncConnection(&m.connectionsData[1], func(_ time.Duration, done func()) { done() })
					m.syncLogID = m.connectionsData[1].ID
				case "layout-usage":
					m.navigate(usagePage)
				case "layout-assistant":
					m.openAssistant()
				case "layout-expanded-detail":
					m.openFullTask(0)
					m.layout.detailExpanded = true
					detail := m.taskDetailUI("refund")
					detail.tab = taskCommentsTab
					detail.comments = []taskComment{{text: tr("复核原支付渠道的退款记录。"), at: "10:30"}}
				case "layout-now-library":
					for len(m.nowSessions) < nowRecentLimit {
						m.newNowSession()
						m.nowSession().title = fmt.Sprintf(tr("会话 %d"), len(m.nowSessions))
						m.nowSession().turns = []nowTurn{{prompt: m.nowSession().title}}
					}
					m.nowLibraryOpen = true
				}
				win.Invalidate()
			} else if file == "tasks-page-2" {
				m.navigate(tasksPage)
				m.closeInspector()
				m.keyboard.palette = false
				m.selectTaskView("")
				m.search, m.statusFilter, m.sourceFilter = "", "全部状态", "全部来源"
				m.taskGrouping = "不分组"
				m.setTaskPage(0)
				m.browseTaskPage(1)
				win.Invalidate()
			} else if strings.HasPrefix(file, "tasks-") {
				m.page = tasksPage
				m.openTask(0)
				switch file {
				case "tasks-narrow":
					m.mutation.Status = "queued"
					win.SetSize(860, 580)
				case "tasks-editing":
					m.mutation.Status, m.editingDue = "confirmed", true
				case "tasks-conflict":
					m.mutation.Status = "conflict"
				case "tasks-unknown":
					m.mutation.Status = "unknown"
				}
				win.Invalidate()
			} else if file == "now-follow-up" {
				m.nowSession().draft = tr("验收通过后，我还需要做什么？")
				m.sendNowTurn()
				win.Invalidate()
			} else if file == "now-after-acceptance" {
				win.SetSize(1190, 730)
				m.nowSession().turns = nil
				m.searchExpanded, m.search = false, ""
				m.completeAcceptance()
				m.showChangeTags = false
				win.Invalidate()
			} else if file == "debug" {
				if m.assistant != nil {
					m.assistant.Close()
				}
				m.page, m.debugOpen, m.showChangeTags = 0, true, true
				win.Invalidate()
			} else if file == "assistant" {
				m.openAssistant()
				win = m.assistant
			} else if file == "settings" {
				m.openSettings()
				win = m.preferences
			} else {
				m.page = i
				m.inspector = false
				if i == tasksPage {
					m.restoreTaskFocus = true
					m.taskSelection = "refund"
					m.locateTaskPage(m.taskRows(), 0)
				}
				win.Invalidate()
			}
		})
		<-ready
		time.Sleep(450 * time.Millisecond)
		data, err := win.CapturePage()
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if err = os.WriteFile(filepath.Join(dir, file+".png"), data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockup) view(c *ui.Context) {
	if m.returnRelatedPending {
		m.returnRelatedPending = false
		m.returnToRelatedSource()
	}
	components.ApplyTheme(c)
	t := c.Theme()
	m.keyboard.editingText = false
	ui.Column(c).Fill().Background(t.Background).ObserveInput(m.keyboard.observe).Children(func() {
		ui.Row(c.Key("header")).FillWidth().Height(50).Padding(12).Gap(8).AlignItems(ui.Center).
			Background(t.Surface).Children(func() {
			ui.Box(c).Width(max(c.TitleBar().Left, 76))
			if m.adaptiveLayout() {
				m.sidebarToggle(c.Key("sidebar-toggle"))
			}
			m.workspaceBreadcrumb(c.Key("breadcrumb"))
			hint := tr("命令面板")
			if keys := m.keyboard.display("command.palette"); keys != "" {
				hint += " · " + keys
			}
			palette := components.IconButton(c.Key("commands"), "command", hint, false)
			for _, item := range m.keyboard.bindings {
				if item.command == "command.palette" && item.second.key == 0 {
					palette.KeyShortcuts(item.first.mods, item.first.key)
				}
			}
			if palette.Clicked() {
				m.showPalette(false)
			}
			if components.IconButton(c.Key("preferences"), "gearshape", tr("设置"), false).Clicked() {
				m.openSettings()
			}
			if components.IconButton(c.Key("assistant"), "sparkles", tr("问助理"), false).Clicked() {
				m.openAssistant()
			}
			if !m.framework {
				m.debugPane(c.Key("debug"))
			}
			if m.unifiedTasks() {
				m.detailToggle(c.Key("detail-toggle"))
			}
		})
		ui.Divider(c)
		ui.Row(c.Key("workspace")).Grow(1).FillWidth().AlignItems(ui.Stretch).Children(func() {
			width, _ := c.Size()
			if !m.layout.sidebarHidden {
				m.sidebar(c.Key("navigation"))
				ui.Box(c.Key("sidebar-divider")).Width(sidebarDivider).Shrink(0).FillHeight().Background(t.Border)
			}
			if m.adaptiveLayout() {
				if !m.layout.sidebarHidden {
					width -= sidebarWidth + sidebarDivider
				}
				m.layoutWorkspace(c.Key("adaptive-workspace"), width)
				return
			}
			if m.page == tasksPage && m.inspector && m.fullTask {
				m.taskPage(c.Key("task-page"))
				return
			}
			if m.inspector && width < 1160 {
				m.inspectorPane(c.Key("detail"), 0)
				return
			}
			m.pageContent(c.Key("main"))
			if m.inspector {
				ui.Box(c.Key("detail-divider")).Width(1).FillHeight().Background(t.Border)
				m.inspectorPane(c.Key("detail"), 300)
			}
			if m.page == tasksPage && !m.inspector && width >= 1160 && m.taskSelection != "" {
				ui.Box(c.Key("preview-divider")).Width(1).FillHeight().Background(t.Border)
				m.taskPreview(c.Key("task-preview"))
			}
		})
		ui.Divider(c.Key("status-divider"))
		m.statusBar(c.Key("status-bar"))
	})
	commands := m.activeCommands()
	m.dispatchKeyboard(c, commands)
	m.commandPalette(c.Key("command-palette"), commands)
	m.assistantDialog(c.Key("assistant-dialog"))
	m.connectorDialog(c.Key("connector-dialog"))
	m.syncLogDialog(c.Key("sync-log-dialog"))
	m.newTaskDialog(c.Key("new-task-dialog"))
	m.savedTaskViewDialog(c.Key("save-view-dialog"))
	m.resourceDialog(c.Key("resource-dialog"))
	m.sourceEditDialog(c.Key("source-edit"))
	m.labelsDialog(c.Key("label-editor"))
}

func (m *mockup) sidebar(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Width(sidebarWidth).Shrink(0).FillHeight().
		Background(t.Background).Children(func() {
		ui.Column(c).FillWidth().Padding(12).Gap(4).Children(func() {
			for i, title := range pages {
				if i == nowPage && m.adaptiveLayout() {
					m.nowSidebar(c.Key("now-section"))
					continue
				}
				entry := ui.ButtonBase(c.Key(title)).Label(tr(title)).FillWidth().Height(34).Radius(6).Justify(ui.Start).Padding(8)
				if keys := m.keyboard.display(pageCommands[i]); keys != "" {
					entry.Tooltip(tr(title) + " · " + keys)
					for _, item := range m.keyboard.bindings {
						if item.command == pageCommands[i] && item.second.key == 0 {
							entry.KeyShortcuts(item.first.mods, item.first.key)
						}
					}
				}
				color := t.TextMuted
				if m.page == i {
					entry.Background(t.Selection)
					color = t.Accent
				} else if entry.Hovered() {
					entry.Background(t.SurfaceHover)
				}
				entry.Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						components.Symbol(c, []string{"bolt.circle", "checklist", "point.3.connected.trianglepath.dotted", "clock", "chart.bar"}[i], color)
						label := components.DataText(c, tr(title))
						if m.page == i {
							label.FontWeight(600)
						}
					})
				})
				if m.keyboard.focusNavigation && m.page == i {
					entry.Focus()
					m.keyboard.focusNavigation = false
				}
				if entry.Clicked() {
					m.navigate(i)
				}
			}
		})
	})
}

func (m *mockup) pageContent(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Padding(16).Gap(12).Background(c.Theme().Surface).Children(func() {
		if m.page == scheduledPage && m.hostPanels.Automation != nil {
			m.hostPanels.Automation(c.Key("automation"))
			return
		}
		if m.page == nowPage {
			if m.nowLibraryOpen {
				m.nowLibrary(c.Key("library"))
			} else {
				m.nowChat(c)
			}
			return
		}
		if !m.adaptiveLayout() && m.page == tasksPage {
			components.Title(c, tr(pages[m.page]))
		}
		ui.Scroll(c.Key(pages[m.page])).Grow(1).FillWidth().TrackScroll(&m.pageScroll[m.page]).Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(func() {
				switch m.page {
				case tasksPage:
					m.tasks(c)
				case connectionsPage:
					m.connections(c)
				case scheduledPage:
					if m.framework {
						components.Muted(c, tr("在 Apilot 默认模式中管理定时任务和运行历史。"))
						return
					}
					m.automation.view(c, m.after)
				case usagePage:
					m.usage(c)
				}
			})
		})
		if m.page == tasksPage {
			m.taskPagination(c.Key("task-pagination"))
		}
	})
}

func (m *mockup) openTask(index int) {
	if !m.inspector || m.selected != index {
		m.editingDue = false
	}
	m.selected, m.inspector = index, true
	if m.page != tasksPage {
		m.fullTask = false
	}
	if m.page == tasksPage {
		m.taskSelection = m.objects[index].ID
	}
}

func (m *mockup) inspectorPane(c *ui.Context, width float32) {
	object := &m.objects[m.selected]
	components.Inspector(c, components.InspectorOptions{
		Title: tr("任务详情"), DetailID: object.ID, Width: width,
		BackLabel: tr("返回") + tr(pages[m.page]), CloseLabel: tr("收起任务详情"),
	}, m.closeInspector, func() { m.taskDetail(c, object) })
}

var taskColumns = []ui.TableColumn{
	{ID: "title", Title: "任务", MinWidth: 130, Sortable: true},
	{ID: "status", Title: "状态", Width: 82, Sortable: true},
	{ID: "priority", Title: "优先级", Width: 62, Sortable: true},
	{ID: "source", Title: "来源", Width: 62, Sortable: true},
	{ID: "due", Title: "截止日", Width: 102, Sortable: true},
	{ID: "updated", Title: "最近变化", Width: 135, Sortable: true},
	{ID: "labels", Title: "标签", Width: 105},
	{ID: "assignee", Title: "经办人", Width: 90, Sortable: true},
	{ID: "file", Title: "文件名", Width: 145, Sortable: true},
	{ID: "path", Title: "路径", Width: 220, Sortable: true},
	{ID: "section", Title: "章节", Width: 140, Sortable: true},
}

func (m *mockup) connectionName(id string) string {
	for _, connection := range m.connectionsData {
		if connection.ID == id {
			return connection.Name
		}
	}
	return id
}

func (m *mockup) taskSourceDescription(o sourceObject) string {
	source := m.sourceText(m.connectionName(o.ConnectionID))
	if o.Mode == "mock" {
		source += " · " + tr("模拟数据")
	}
	return displayTaskID(o) + " · " + source
}

func (m *mockup) taskValues(o sourceObject) []string {
	due := tr(o.NativeFields["due"])
	if due == "" {
		due = "—"
	}
	status := m.taskStateLabel(o)
	if o.ID == m.mutation.SourceObject {
		switch m.mutation.Status {
		case "queued":
			due += tr(" · 待回写")
		case "conflict":
			due += tr(" · 冲突")
		case "unknown":
			due += tr(" · 待核对")
		}
	}
	updated := "—"
	if len(o.Activity) > 0 {
		updated = taskActivityTime(o.Activity[len(o.Activity)-1])
	}
	return []string{m.sourceText(o.Title), status, tr(o.NativeFields["priority"]), m.taskSourceDescription(o), due, updated, strings.Join(taskLabelValues(o), ", "), strings.Join(o.Assignees, ", "), strings.Join(m.searchValues(o, "file"), ""), o.NativeFields["path"], o.NativeFields["section"]}
}

func (m *mockup) matchesStatusFilter(task sourceObject) bool {
	if m.live != nil {
		return m.statusFilter == "全部状态" || m.statusFilter == workbenchStateLabel(task.State)
	}
	if m.statusFilter == "待处理" || m.statusFilter == "进行中" {
		return m.taskStateLabel(task) == tr(m.statusFilter)
	}
	return (m.statusFilter != "未完成" || !taskClosed(task)) && (m.statusFilter != "已完成" || taskClosed(task))
}

func (m *mockup) taskRows() []int {
	query, problem := m.parseSearch()
	if problem != "" {
		return nil
	}
	var rows []int
	for i, o := range m.objects {
		if m.taskWatchOnly && !m.isWatched(o.ID) {
			continue
		}
		if !m.matchesStatusFilter(o) ||
			m.sourceFilter != "全部来源" && m.sourceFilter != m.connectionName(o.ConnectionID) {
			continue
		}
		if m.matchesTask(o, query) {
			rows = append(rows, i)
		}
	}
	column := 0
	for i, candidate := range taskColumns {
		if candidate.ID == m.taskSort.Column {
			column = i
			break
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := m.objects[rows[i]], m.objects[rows[j]]
		if group := strings.Compare(m.taskGroup(a), m.taskGroup(b)); group != 0 {
			return group < 0
		}
		order := 0
		switch taskColumns[column].ID {
		case "updated":
			order = taskUpdated(a).Compare(taskUpdated(b))
		case "due":
			dateA := parseTaskDate(a.NativeFields["due"])
			dateB := parseTaskDate(b.NativeFields["due"])
			if dateA.IsZero() != dateB.IsZero() {
				return !dateA.IsZero()
			}
			order = dateA.Compare(dateB)
		case "priority":
			rank := map[string]int{"最低": 1, "低": 2, "普通": 3, "中": 4, "高": 5, "最高": 6, "lowest": 1, "low": 2, "normal": 3, "medium": 4, "high": 5, "highest": 6}
			order = cmp.Compare(rank[a.NativeFields["priority"]], rank[b.NativeFields["priority"]])
		default:
			order = strings.Compare(m.taskValues(a)[column], m.taskValues(b)[column])
		}
		if m.taskSort.Descending {
			return order > 0
		}
		return order < 0
	})
	return rows
}

func (m *mockup) tasks(c *ui.Context) {
	m.taskListControls(c.Key("controls"))
	m.taskListResults(c.Key("results"), 0)
}

func (m *mockup) taskListControls(c *ui.Context) {
	compact := m.compactTaskList()
	m.taskViewBar(c.Key("task-views"))
	m.changeTag(c, "task-search", tr("按来源和状态组合筛选，点击表头排序。"))
	ui.Row(c.Key("filters")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		components.Symbol(c.Key("search-icon"), "magnifyingglass", c.Theme().TextMuted)
		search := ui.TextInput(c.Key("search"), &m.search).Label(tr("搜索任务")).Placeholder(tr("搜索任务")).Grow(1)
		if m.keyboard.focusSearch {
			search.Focus()
			m.keyboard.focusSearch = false
		}
		m.keyboard.editingText = search.Focused()
		m.sourceLogoFilters(c.Key("source"))
		if !compact {
			m.taskFilterSelects(c.Key("selects"))
		}
		if m.unifiedTasks() {
			m.openTaskButton(c.Key("open-task"))
		}
		filtered := m.statusFilter != "全部状态" || m.sourceFilter != "全部来源"
		fields := components.IconToggle(c.Key("search-fields"), "line.3.horizontal.decrease.circle", tr("搜索字段"), m.searchExpanded || (compact && filtered))
		if fields.Clicked() {
			m.searchExpanded = !m.searchExpanded
		}
		if compact {
			m.taskControlsPopover(c.Key("filter-panel"), fields, &m.searchExpanded, func() {
				ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() { m.taskFilterSelects(c.Key("selects")) })
				m.sourceQuickFilters(c.Key("source-quick-filters"))
				m.searchHelp(c.Key("search-help"))
			})
		}
	})
}

func (m *mockup) taskFilterSelects(c *ui.Context) {
	states := []string{"全部状态", "未完成", "待处理", "进行中", "已完成"}
	if m.live != nil {
		states = []string{"全部状态", "待处理", "进行中", "已完成", "不计划处理", "未知"}
	}
	localizedSelect(c.Key("status"), &m.statusFilter, states, 110)
}

func (m *mockup) sourceLogoFilters(c *ui.Context) {
	if len(m.connectionsData) == 0 {
		return
	}
	width := min(180, 36*(len(m.connectionsData)+1))
	ui.ScrollHorizontal(c).Width(float32(width)).Height(30).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			all := m.sourceFilter == "全部来源"
			if components.IconToggle(c.Key("all"), "square.grid.2x2", tr("全部来源"), all).Clicked() {
				m.sourceFilter = "全部来源"
			}
			for _, connection := range m.connectionsData {
				name := tr(connection.Name)
				selected := m.sourceFilter == connection.Name
				button := ui.ButtonBase(c.Key(connection.ID)).Label(name).Tooltip(name).Size(32, 30).Radius(6).Center().Selected(selected)
				if selected {
					button.Background(c.Theme().Selection)
				} else if button.Hovered() {
					button.Background(c.Theme().SurfaceHover)
				}
				button.Children(func() {
					opacity := float32(0.45)
					if all || selected {
						opacity = 1
					}
					ui.Box(c).Opacity(opacity).Children(func() { connectorLogo(c, connection.SourceKind, 16) })
				})
				if button.Clicked() {
					if selected {
						m.sourceFilter = "全部来源"
					} else {
						m.sourceFilter = connection.Name
					}
				}
			}
		})
	})
}

func (m *mockup) taskListResults(c *ui.Context, viewportHeight float32) {
	autoFit := viewportHeight > 0 && m.unifiedTasks() && m.taskPageSizeOverride == 0
	if !m.compactTaskList() && (m.sourceFilter != "全部来源" || m.searchExpanded) {
		accessories := ui.Column(c.Key("result-filters")).FillWidth().Gap(taskResultsGap)
		accessories.Children(func() {
			m.sourceQuickFilters(c.Key("source-quick-filters"))
			if m.searchExpanded {
				m.searchHelp(c.Key("search-help"))
			}
		})
		viewportHeight -= accessories.LayoutBounds().H + taskResultsGap
	}
	_, problem := m.parseSearch()
	if problem != "" {
		components.StatusLabel(c.Key("query-error"), components.Status{Text: problem, Icon: "exclamationmark.circle", Color: c.Theme().Warning})
	}
	if m.liveError {
		components.Muted(c.Key("live-error"), tr("无法读取工作台数据"))
	}
	rows := m.taskRows()
	filtered := m.search != "" || m.statusFilter != "全部状态" || m.sourceFilter != "全部来源" || m.taskWatchOnly
	if m.live != nil && (len(rows) > 0 || filtered) && m.hasUnavailableTaskSource() {
		m.connectionGuidance(c.Key("partial-sources"), tr("部分来源无法同步，列表可能不完整。"))
	}
	if !m.unifiedTasks() && m.inspector && !slices.Contains(rows, m.selected) {
		m.closeInspector()
	}
	if len(rows) == 0 {
		m.taskSelection = ""
	} else {
		if autoFit {
			m.fitTaskPage(c.Theme(), rows, viewportHeight)
		}
	}
	m.taskTables(c.Key("tables"), m.pagedTaskRows(rows))

	if m.live != nil && problem == "" && len(rows) == 0 {
		m.liveTaskEmptyState(c.Key("empty-tasks"))
	} else if m.framework && len(m.objects) == 0 && problem == "" {
		ui.Column(c.Key("empty-tasks")).FillWidth().Padding(0, 8).Gap(8).Children(func() {
			components.Heading(c, tr("暂无任务"))
			m.connectionGuidance(c.Key("guidance"), tr("任务会在来源同步后出现在这里。"))
		})
	} else if len(rows) == 0 {
		ui.Row(c.Key("no-results")).Gap(8).AlignItems(ui.Center).Children(func() {
			if problem == "" {
				components.Muted(c, tr("没有符合条件的任务"))
			}
			if components.IconButton(c.Key("clear"), "xmark.circle", tr("清除筛选"), false).Clicked() {
				m.search, m.statusFilter, m.sourceFilter = "", "全部状态", "全部来源"
			}
		})
	} else if !m.unifiedTasks() {
		ui.Row(c.Key("result-summary")).AlignItems(ui.Center).Gap(8).Children(func() {
			width, _ := c.Size()
			if width < 1160 && m.taskSelection != "" && components.IconButton(c.Key("open"), "arrow.up.right", tr("打开完整任务"), false).Clicked() {
				index := slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == m.taskSelection })
				if index >= 0 {
					m.openFullTask(index)
				}
			}
		})
	}
}

func (m *mockup) liveTaskEmptyState(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(0, 8).Gap(8).AlignItems(ui.Start).Children(func() {
		switch {
		case m.liveError:
			components.Muted(c, tr("暂时无法确认任务列表，请检查连接。"))
			button := ui.Button(c.Key("connections"), tr("查看连接器"))
			m.focusTaskConnectionEntry(func() { button.Focus() })
			if button.Clicked() {
				m.openTaskConnections()
			}
		case !m.liveLoaded:
			components.Muted(c, tr("正在读取任务…"))
		case m.search != "" || m.statusFilter != "全部状态" || m.sourceFilter != "全部来源" || m.taskWatchOnly:
			components.Muted(c, tr("没有符合条件的任务"))
			if ui.Button(c.Key("clear"), tr("清除筛选")).Clicked() {
				m.search, m.statusFilter, m.sourceFilter, m.taskWatchOnly = "", "全部状态", "全部来源", false
			}
		default:
			m.unfilteredLiveTaskEmptyState(c)
		}
	})
}

func (m *mockup) hasUnavailableTaskSource() bool {
	for _, conn := range m.connectionsData {
		if conn.SourceKind != common.WorkbenchLocal && (conn.Status == "同步失败" || conn.Status == "连接断开" || conn.Paused || conn.PluginDisabled) {
			return true
		}
	}
	return false
}

func (m *mockup) unfilteredLiveTaskEmptyState(c *ui.Context) {
	external, pending, waiting := false, false, false
	for _, conn := range m.connectionsData {
		if conn.SourceKind == common.WorkbenchLocal {
			continue
		}
		external = true
		pending = pending || m.pendingSync[conn.ID]
		waiting = waiting || conn.Status == "等待首次同步"
	}
	components.Heading(c, tr("暂无任务"))
	switch {
	case m.hasUnavailableTaskSource():
		m.connectionGuidance(c.Key("guidance"), tr("部分来源无法同步，请检查连接状态。"))
	case pending:
		m.connectionGuidance(c.Key("guidance"), tr("来源正在同步，任务确认后会显示在这里。"))
	case waiting:
		m.connectionGuidance(c.Key("guidance"), tr("来源尚未完成首次同步。"))
	case !external:
		components.Muted(c.Key("guidance"), tr("添加连接后，来源任务会显示在这里。"))
		button := ui.Button(c.Key("add-connection"), tr("添加连接"))
		m.focusTaskConnectionEntry(func() { button.Focus() })
		if button.Clicked() {
			m.openTaskConnections()
		}
	default:
		m.connectionGuidance(c.Key("guidance"), tr("已确认的来源中没有任务。"))
	}
}

func (m *mockup) taskDetail(c *ui.Context, o *sourceObject) {
	components.Title(c, m.sourceText(o.Title))
	components.Muted(c, m.sourceText(o.Locator))
	ui.Row(c.Key("facts")).Gap(8).AlignItems(ui.Center).Children(func() {
		components.DataText(c, tr(o.NativeFields["status"]))
		components.Muted(c, tr("优先级 ")+tr(o.NativeFields["priority"]))
	})
	if evidence := o.NativeFields["excerpt"]; evidence != "" {
		ui.Divider(c)
		components.SectionHeading(c.Key("source-heading"), "doc.text", tr("来源摘录"))
		components.Body(c, tr(evidence))
	}
	m.taskSourceActions(c.Key("source-actions"), o)
	m.taskWatchButton(c.Key("watch"), o)
	m.taskProperties(c, o)
}

func (m *mockup) taskProperties(c *ui.Context, o *sourceObject) {
	if o.ID == m.mutation.SourceObject {
		if !m.pendingTaskEdits[o.ID] {
			m.writeback(c, o)
		}
	} else if due := o.NativeFields["due"]; due != "" {
		components.PropertyGrid(c.Key("due"), []components.Property{{ID: "due", Title: tr("截止日"), Value: tr(due)}}, 2)
	}
	m.obsidianSourceLink(c.Key("obsidian-link"), o)
	m.taskDateProperties(c.Key("dates"), o)
	m.taskLabelProperty(c.Key("labels"), o)
	if conn := m.taskConnection(*o); conn != nil {
		var properties []components.Property
		for _, field := range conn.SearchFields {
			switch field {
			case "created", "start", "scheduled", "done", "cancelled", "created_at", "updated_at", "closed_at":
				continue
			}
			if value := strings.Join(m.searchValues(*o, field), ", "); strings.TrimSpace(value) != "" && field != "checkbox" {
				wide := field == "path" || field == "repo"
				properties = append(properties, components.Property{ID: field, Title: tr(searchFieldTitle(field)), Value: value, Wide: wide})
			}
		}
		components.PropertyGrid(c.Key("fields"), properties, 2)
	}
}

func (m *mockup) taskDateProperties(c *ui.Context, o *sourceObject) {
	fields := o.NativeFields
	updated := fields["updated_at"]
	if len(o.Activity) > 0 {
		latest := o.Activity[len(o.Activity)-1]
		nativeTime, _ := time.Parse(time.RFC3339, updated)
		if updated == "" || latest.Time.After(nativeTime) {
			updated = taskActivityTime(latest)
		}
	}
	var dates []components.Property
	for _, item := range [][3]string{
		{"created", "创建时间", fields["created_at"]},
		{"created-date", "创建日期", fields["created"]},
		{"cancelled", "取消日期", fields["cancelled"]},
		{"updated", "更新时间", updated},
		{"start", "开始日期", fields["start"]},
		{"scheduled", "计划日期", fields["scheduled"]},
		{"done", "完成日期", fields["done"]},
		{"closed", "关闭时间", fields["closed_at"]},
	} {
		if item[2] != "" {
			value := tr(item[2])
			if stamp, err := time.Parse(time.RFC3339, value); err == nil {
				value = stamp.Format("2006-01-02 15:04")
			}
			dates = append(dates, components.Property{ID: item[0], Title: tr(item[1]), Value: value})
		}
	}
	components.PropertyGrid(c, dates, 2)
}

func (m *mockup) writeback(c *ui.Context, o *sourceObject) {
	m.changeTag(c, "writeback", tr("回写状态独立于来源确认值。"))
	if m.mutation.Status == "conflict" {
		components.Panel(c.Key("conflict"), func() {
			components.Heading(c, tr("截止日发生冲突"))
			m.writebackValues(c, o)
			ui.Row(c).Wrap().Gap(8).Children(func() {
				if ui.Button(c.Key("keep"), tr("保留来源值")).Clicked() {
					m.mutation.DesiredFields["due"] = o.NativeFields["due"]
					m.dueDraft = o.NativeFields["due"]
					m.mutation.Status, m.state = "confirmed", 2
				}
				if ui.PrimaryButton(c.Key("write"), tr("提交本地值")).Clicked() {
					m.queueWrite()
				}
			})
		})
		return
	}
	ui.Column(c.Key("mutation")).FillWidth().Gap(8).Children(func() {
		switch m.mutation.Status {
		case "queued":
			components.StatusLabel(c.Key("pending"), components.Status{Text: tr("等待来源确认"), Loading: true, Color: c.Theme().Warning})
			m.writebackValues(c, o)
		case "confirmed":
			if !m.editingDue {
				ui.Row(c).FillWidth().Wrap().Gap(8).AlignItems(ui.Center).Children(func() {
					components.DataText(c, tr("截止日  ")+tr(o.NativeFields["due"]))
					if components.IconButton(c.Key("edit"), "calendar.badge.clock", tr("编辑截止日"), false).Clicked() {
						m.beginDeadlineEdit(o)
					}
				})
				return
			}
			ui.Field(c.Key("due-field"), tr("截止日"), func() {
				localizedSelect(c.Key("due"), &m.dueDraft, []string{"11 月 19 日", "11 月 20 日", "11 月 23 日"}, 136)
			})
			ui.Row(c).Wrap().Gap(8).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					m.dueDraft = o.NativeFields["due"]
					m.editingDue = false
				}
				if ui.PrimaryButton(c.Key("save"), tr("保存截止日")).Disabled(m.dueDraft == o.NativeFields["due"]).Clicked() {
					m.mutation.DesiredFields["due"] = m.dueDraft
					m.editingDue = false
					m.queueWrite()
				}
			})

		case "unknown":
			components.StatusLabel(c.Key("unknown"), components.Status{Text: tr("回写结果待核对"), Icon: "exclamationmark.circle", Color: c.Theme().Warning})
			m.writebackValues(c, o)
			if ui.Button(c.Key("verify"), tr("核对来源截止日")).Clicked() {
				m.dueDraft = m.mutation.DesiredFields["due"]
				if o.NativeFields["due"] == m.dueDraft {
					m.mutation.Status, m.state = "confirmed", 2
				} else {
					m.mutation.Status, m.state = "conflict", 0
				}
			}
		}
	})
}

func (m *mockup) beginDeadlineEdit(o *sourceObject) {
	if !m.editingDue {
		m.dueDraft = o.NativeFields["due"]
		m.editingDue = true
	}
}

func (m *mockup) writebackValues(c *ui.Context, o *sourceObject) {
	components.DataText(c, tr("来源已确认  ")+tr(o.NativeFields["due"]))
	components.DataText(c, tr("拟写入  ")+tr(m.mutation.DesiredFields["due"]))
}

// queueWrite simulates the round trip in normal preview; debug can override it.
func (m *mockup) queueWrite() {
	m.writeGeneration++
	generation := m.writeGeneration
	m.mutation.Status, m.state = "queued", 1
	m.after(900*time.Millisecond, func() {
		if m.writeGeneration != generation || m.mutation.Status != "queued" {
			return
		}
		value := m.mutation.DesiredFields["due"]
		m.objects[m.selectedMutationObject()].NativeFields["due"] = value
		m.dueDraft = value
		m.mutation.Status, m.state = "confirmed", 2
		m.recordTaskEvent(m.mutation.SourceObject, "来源确认了截止日修改")
	})
}

func (m *mockup) selectedMutationObject() int {
	for i, object := range m.objects {
		if object.ID == m.mutation.SourceObject {
			return i
		}
	}
	panic("mock mutation references an unknown object")
}

func (m *mockup) after(delay time.Duration, complete func()) {
	time.AfterFunc(delay, func() {
		mygo.RunOnMain(func() {
			if m.win != nil && m.win.IsDestroyed() {
				return
			}
			complete()
			if m.win != nil {
				m.win.Invalidate()
			}
		})
	})
}

func connectionField(c *ui.Context, icon, label, value string) {
	ui.Column(c).Grow(1).Gap(6).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			components.Symbol(c, icon, c.Theme().TextMuted)
			components.Muted(c, label)
		})
		ui.Text(c, tr(value)).FontSize(13)
	})
}

func (m *mockup) openAssistant() {
	if m.adaptiveLayout() {
		m.assistantContext = m.currentTaskIndex()
		m.assistantScroll = ui.ScrollState{}
		m.assistantOpen, m.assistantNeedsFocus = true, true
		if m.win != nil {
			m.win.Invalidate()
		}
		return
	}
	if m.assistant != nil && !m.assistant.IsDestroyed() {
		m.assistant.Focus()
		return
	}
	if m.inspector {
		m.assistantContext = m.selected
	}
	m.assistant = mygo.NewWindow(mygo.WindowOptions{
		Title: tr("问助理 · 模拟模式"), Width: 600, Height: 410, Parent: m.win, Modal: true,
		Content: ui.View(m.assistantView),
	})
	m.assistant.Show()
}

type assistantTurn struct {
	question, reply string
}

type assistantThread struct {
	draft  string
	turns  []assistantTurn
	scroll ui.ScrollState
}

func (m *mockup) assistantThread() *assistantThread {
	return m.assistantThreadFor(m.assistantContext)
}

func (m *mockup) assistantThreadFor(index int) *assistantThread {
	id := m.objects[index].ID
	thread := m.assistantThreads[id]
	if thread == nil {
		thread = &assistantThread{}
		m.assistantThreads[id] = thread
	}
	return thread
}

func (m *mockup) assistantView(c *ui.Context) {
	components.ApplyTheme(c)
	index := m.assistantContext
	object := m.objects[index]
	thread := m.assistantThread()
	scroll := &thread.scroll
	if m.adaptiveLayout() {
		scroll = &m.assistantScroll
	}
	openContext := func() {
		if m.page == tasksPage && !slices.Contains(m.taskRows(), index) {
			m.search, m.statusFilter, m.sourceFilter = "", "全部状态", "全部来源"
		}
		if m.adaptiveLayout() {
			m.openNowTask(index)
			return
		}
		m.openTask(index)
		if m.assistant != nil {
			m.assistant.Close()
			if m.win != nil {
				m.win.Invalidate()
			}
		}
	}
	content := ui.Column(c).Gap(12)
	if m.adaptiveLayout() {
		content.Grow(1).FillWidth().MinHeight(0)
	} else {
		content.Fill().Padding(20)
	}
	content.Children(func() {
		components.Title(c.Key("assistant-title"), m.sourceText(object.Title))
		components.Muted(c, tr(m.provider)+tr(" · 模拟回复"))
		m.changeTag(c, "dialog", tr("对话保留上下文和来源引用。"))
		ui.Divider(c)
		ui.Scroll(c.Key(object.ID)).Grow(1).MinHeight(0).FillWidth().TrackScroll(scroll).Children(func() {
			ui.Column(c).FillWidth().Gap(16).Children(func() {
				if len(thread.turns) == 0 {
					if m.taskReference(c.Key("context-source"), index) {
						openContext()
					}
				}
				for i, turn := range thread.turns {
					components.ConversationTurn(c.Key(fmt.Sprintf("turn-%d", i)), turn.question, turn.reply, func() {
						if m.taskReference(c.Key("source"), index) {
							openContext()
						}
					})
				}
			})
		})
		m.taskAssistantComposer(c.Key("composer"), &object, thread, false)
	})
}

func (m *mockup) taskAssistantComposer(c *ui.Context, object *sourceObject, thread *assistantThread, inline bool) {
	if m.live != nil {
		return
	}
	group := ui.Column(c).FillWidth()
	group.Children(func() {
		if question := components.Composer(c.Key("input"), &thread.draft, components.ComposerLabels{
			Input: tr("询问当前工作"), Placeholder: tr("询问此任务…"), Send: tr("发送"),
			Focus: (inline && m.layout.focusAssistant) || (!inline && m.assistantNeedsFocus),
		}); question != "" {
			reply := fmt.Sprintf(tr("当前状态为「%s」。"), tr(object.NativeFields["status"]))
			thread.turns = append(thread.turns, assistantTurn{question: question, reply: reply})
			if inline {
				m.taskDetailUI(object.ID).tab = taskAssistantTab
			}
			thread.draft = ""
			scroll := &thread.scroll
			if !inline && m.adaptiveLayout() {
				scroll = &m.assistantScroll
			}
			scroll.ScrollTo(0, math.MaxFloat32, ui.Animation{})
		}
	})
	if inline {
		m.layout.focusAssistant = false
		m.keyboard.editingText = m.keyboard.editingText || group.FocusWithin()
	} else {
		m.assistantNeedsFocus = false
	}
}
