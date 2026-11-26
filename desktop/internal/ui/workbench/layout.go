package workbench

import (
	"slices"

	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

const (
	sidebarWidth    = 188
	sidebarDivider  = 1
	layoutListMin   = 488
	layoutDetailMin = 352
	layoutBriefSize = 54
	taskResultsGap  = 8
)

type taskViewportGeometry struct {
	windowHeight, listWidth, stackShare float32
	views                               int
	source, status                      string
	stacked, detailHidden               bool
	displayOptions, searchFields, tags  bool
}

type workbenchLayout struct {
	adaptive, sidebarHidden      bool
	detailHidden, detailExpanded bool
	detailReturnHidden           bool
	sideShare, stackShare        float32
	listWidth, detailWidth       float32
	listHeight                   float32
	focusDetail, focusAssistant  bool
	stacked, revealSelection     bool
	taskViewport                 taskViewportGeometry
}

func (m *mockup) adaptiveLayout() bool { return m.layout.adaptive }

func (m *mockup) useAdaptiveLayout() {
	m.layout = workbenchLayout{adaptive: true, sideShare: 57, stackShare: 52}
	m.page = tasksPage
}

func (m *mockup) unifiedTasks() bool {
	return m.adaptiveLayout() && m.page == tasksPage
}

func (m *mockup) sidebarToggle(c *ui.Context) {
	label := "收起侧边栏"
	if m.layout.sidebarHidden {
		label = "显示侧边栏"
	}
	button := components.IconToggle(c, "sidebar.left", tr(label), !m.layout.sidebarHidden)
	if m.layout.sidebarHidden && m.keyboard.focusNavigation {
		button.Focus()
		m.keyboard.focusNavigation = false
	}
	if button.Clicked() {
		m.layout.sidebarHidden = !m.layout.sidebarHidden
	}
}

func (m *mockup) layoutBrief(c *ui.Context) {
	target, summary, action := 0, "周五答复客户前，退款验收仍在进行", "查看验收依据"
	switch {
	case len(m.objects) == 0:
		summary, action = "暂无需要处理的新变化", ""
	case taskClosed(m.objects[0]):
		target, summary, action = 1, "退款验收通过，客户答复待处理", "查看客户答复任务"
	case m.variant == 1:
		summary, action = "暂无需要处理的新变化", ""
	case m.briefDismissed:
		target, summary, action = 2, "#40  退款状态说明已更新", "查看任务"
	}

	ui.Row(c).Height(layoutBriefSize).Shrink(0).FillWidth().AlignItems(ui.Center).Padding(0, 16).Gap(12).
		Background(c.Theme().Background).Children(func() {
		ui.Text(c.Key("now"), "Now").FontWeight(600)
		components.DataText(c.Key("summary"), tr(summary)).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if action != "" && ui.Button(c.Key("task"), tr(action)).Clicked() {
			m.openFullTask(target)
		}
	})
}

func (m *mockup) layoutWorkspace(c *ui.Context, width float32) {
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().MinHeight(0).Background(c.Theme().Surface).Children(func() {
		if !m.unifiedTasks() {
			m.pageContent(c)
			return
		}
		if m.layout.detailExpanded && !m.layout.detailHidden && m.currentTaskIndex() >= 0 {
			m.layoutTaskDetail(c.Key("expanded-detail"))
			return
		}
		_, height := c.Size()
		availableHeight := height - 51 - statusBarHeight - 1
		if m.live == nil {
			m.layoutBrief(c.Key("brief"))
			ui.Divider(c)
			availableHeight -= layoutBriefSize + 1
		}
		m.layout.listHeight = availableHeight
		if m.layout.detailHidden || m.taskBoard {
			m.layout.listWidth = width
			m.layoutTaskList(c.Key("list-only"))
			return
		}
		firstMin, secondMin := float32(layoutListMin), float32(layoutDetailMin)
		stacked := width < firstMin+secondMin+1
		if stacked != m.layout.stacked {
			m.layout.stacked = stacked
			m.layout.revealSelection = m.taskSelection != ""
		}
		share, size, reset := &m.layout.sideShare, width, float32(57)
		m.layout.listWidth = width
		m.layout.detailWidth = width
		if stacked {
			share, size = &m.layout.stackShare, availableHeight
			firstMin, secondMin, reset = 220, 160, 52
			m.layout.listHeight = (size - 1) * components.SplitShare(*share, size, firstMin, secondMin) / 100
		} else {
			m.layout.listWidth = (width - 1) * components.SplitShare(*share, size, firstMin, secondMin) / 100
			m.layout.detailWidth = width - 1 - m.layout.listWidth
		}
		components.AdaptiveSplit(c.Key("split"), stacked, share, size, firstMin, secondMin, reset, func() {
			m.layoutTaskList(c.Key("list"))
		}, func() {
			m.layoutTaskDetail(c.Key("detail"))
		})
	})
}

func (m *mockup) detailToggle(c *ui.Context) {
	label := "收起任务详情"
	if m.layout.detailHidden {
		label = "显示任务详情"
	}
	if components.IconToggle(c, "sidebar.right", tr(label), !m.layout.detailHidden).Clicked() {
		m.layout.detailHidden = !m.layout.detailHidden
		if m.layout.detailHidden {
			m.layout.detailExpanded = false
			m.closeInspector()
		}
		c.Invalidate()
	}
}

func (m *mockup) layoutTaskList(c *ui.Context) {
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Padding(12).Gap(8).Children(func() {
		m.taskListControls(c.Key("controls"))
		if m.taskBoard {
			m.taskListResults(c.Key("board-results"), 0)
			return
		}
		scroll := ui.Scroll(c.Key("tasks")).Grow(1).MinHeight(0).FillWidth().TrackScroll(&m.pageScroll[tasksPage])
		scroll.Children(func() {
			ui.Column(c).FillWidth().Gap(taskResultsGap).Children(func() {
				m.taskListResults(c.Key("results"), scroll.LayoutBounds().H)
			})
		})
		m.taskPagination(c.Key("pagination"))
		_, height := c.Size()
		geometry := taskViewportGeometry{
			windowHeight: height, listWidth: m.layout.listWidth, stackShare: m.layout.stackShare, views: len(m.savedTaskViews),
			source: m.sourceFilter, status: m.statusFilter, stacked: m.layout.stacked, detailHidden: m.layout.detailHidden, displayOptions: m.taskDisplayOpen, searchFields: m.searchExpanded, tags: m.showChangeTags,
		}
		if geometry != m.layout.taskViewport {
			m.layout.taskViewport = geometry
			// MyGo exposes element bounds from the previous frame.
			c.Invalidate()
		}
	})
}

func (m *mockup) layoutTaskDetail(c *ui.Context) {
	index := slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == m.taskSelection })
	if m.inspector {
		index = m.selected
	}
	ui.Column(c).Fill().MinWidth(0).MinHeight(0).Padding(12).Gap(8).Children(func() {
		if index < 0 {
			ui.Scroll(c.Key("brief-thread")).Grow(1).MinHeight(0).FillWidth().TrackScroll(&m.nowSession().scroll).Children(func() {
				ui.Column(c).FillWidth().Gap(8).Children(func() { m.nowChatContent(c) })
			})
			if !m.framework {
				m.nowComposer(c, m.layout.focusAssistant)
			}
			return
		}

		object := &m.objects[index]
		m.taskBackLink(c.Key("return-to-source"))
		ui.Row(c.Key("title-row")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			if m.taskBoard && m.layout.detailExpanded {
				if components.IconButton(c.Key("back-to-board"), "chevron.left", tr("返回看板"), false).Clicked() {
					m.leaveTaskDetail()
					c.Invalidate()
				}
			}
			title := ui.Text(c.Key("title"), m.sourceText(object.Title)).FontSize(18).FontWeight(600).SingleLine().Ellipsis("…").
				Tooltip(m.sourceText(object.Title)).Focusable().Grow(1).MinWidth(0)
			if m.layout.focusDetail {
				title.Focus()
				m.layout.focusDetail = false
			}
			if !m.taskBoard {
				m.detailSizeButton(c.Key("detail-size"))
			}
		})
		ui.Row(c.Key("actions")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			caption := displayTaskID(*object) + " · " + m.taskStateLabel(*object)
			if object.Mode == "mock" {
				caption += " · " + tr("模拟数据")
			}
			ui.Text(c, caption).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
			m.taskWatchButton(c.Key("watch"), object)
			m.taskSourceControls(c.Key("edit"), object)
		})
		m.taskSourceFeedback(c.Key("write-feedback"), object)
		ui.Divider(c)
		thread := m.assistantThreadFor(index)
		detail := m.taskDetailUI(object.ID)
		scrollKey := c.Key(object.ID)
		ui.Scroll(scrollKey).Grow(1).MinHeight(0).FillWidth().TrackScroll(&thread.scroll).Children(func() {
			ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Column(c).FillWidth().MaxWidth(760).Gap(12).Children(func() {
					m.connectorTaskHeader(c.Key("connector-header"), object, true)
					m.taskDescription(c.Key("description"), object)
					m.taskPageProperties(c.Key("properties"), object)
					m.taskDetailTabs(c.Key("body"), object, detail, thread)
				})
			})
		})
		if detail.tab == taskCommentsTab {
			m.taskCommentComposer(c.Key("comment"), object, detail, &thread.scroll)
		} else {
			m.taskAssistantComposer(c.Key("composer"), object, thread, true)
		}
	})
}

func (m *mockup) layoutShowsTaskColumn(id string) bool {
	if !m.unifiedTasks() {
		return true
	}
	switch id {
	case "updated":
		return m.layout.listWidth >= 620
	case "due":
		return m.layout.listWidth >= 510
	case "file":
		return m.layout.listWidth >= 580
	case "priority", "labels", "assignee":
		return m.layout.listWidth >= 580 || m.taskSort.Column == id
	default:
		return true
	}
}

func (m *mockup) compactTaskList() bool {
	return m.unifiedTasks() && (m.layout.listWidth < 560 || m.layout.listHeight < 350)
}

func (m *mockup) taskControlsPopover(c *ui.Context, anchor ui.Element, open *bool, content func()) {
	width, height := c.Size()
	ui.Popover(c, anchor, open, func() {
		ui.Scroll(c).Width(min(width-68, 350)).MaxHeight(min(height*0.42, 260)).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(content)
		})
	})
}
