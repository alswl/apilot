package workbench

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type nowTurn struct {
	prompt     string
	reply      string
	acceptance bool
	translated bool
}

func (m *mockup) nowChat(c *ui.Context) {
	m.nowConversation(c, &m.nowSession().scroll, m.layout.focusAssistant, true)
}

func (m *mockup) nowConversation(c *ui.Context, scroll *ui.ScrollState, focusComposer, showReviews bool) {
	ui.Scroll(c.Key(m.nowSessionIndex)).Grow(1).MinHeight(0).FillWidth().TrackScroll(scroll).Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Column(c.Key("messages")).FillWidth().MaxWidth(688).Padding(12, 0).Gap(24).Children(func() {
				if showReviews {
					m.nowReviewRequests(c.Key("review-requests"), mygo.Shell.OpenExternal)
				}
				m.nowChatContent(c)
			})
		})
	})
	if !m.framework {
		m.nowComposer(c.Key("composer-region"), focusComposer)
	}
}

func (m *mockup) nowChatContent(c *ui.Context) {
	session := m.nowSession()
	if session.briefing {
		m.nowBrief(c.Key("brief"))
	} else if len(session.turns) == 0 {
		components.ChatReply(c.Key("start"), "Apilot", "", func() {
			if m.framework {
				if m.assistantOpen {
					components.Body(c, tr("助理尚未接入"))
					components.Muted(c, tr("接入推理服务后，可在这里提问。"))
				} else {
					components.Body(c, tr("暂无简报"))
					m.connectionGuidance(c.Key("guidance"), tr("来源同步后，Now 会汇集值得关注的变化。"))
				}
			} else {
				components.Body(c, tr("想跟进哪项工作？"))
			}
		})
	}
	for i, turn := range session.turns {
		key := c.Key(fmt.Sprintf("turn-%d", i))
		if turn.acceptance {
			m.nowAcceptance(key)
		} else {
			prompt, reply := turn.prompt, turn.reply
			if turn.translated {
				prompt, reply = tr(prompt), tr(reply)
			}
			components.ChatTurn(key, prompt, reply, nil)
		}
	}
}

func (m *mockup) nowComposer(c *ui.Context, focus bool) {
	composer := ui.Row(c).FillWidth().Justify(ui.Center)
	composer.Children(func() {
		ui.Column(c).FillWidth().MaxWidth(688).Padding(8, 12).Border(1, c.Theme().Border).Radius(8).Background(c.Theme().Surface).Children(func() {
			if question := components.Composer(c.Key("composer"), &m.nowSession().draft, components.ComposerLabels{
				Input: tr("追问这份简报"), Placeholder: tr("发消息给 Apilot…"), Send: tr("发送追问"), Focus: focus,
			}); question != "" {
				m.sendNowTurn()
			}
		})
	})
	m.layout.focusAssistant = false
	m.keyboard.editingText = m.keyboard.editingText || composer.FocusWithin()
}

func (m *mockup) nowBrief(c *ui.Context) {
	components.ChatReply(c, "Apilot", tr("今天 09:42"), func() {
		m.changeTag(c, "prepared", tr("打开 Now 即可看到事先准备好的简报。"))
		if m.variant == 1 {
			components.Body(c, tr("暂无需要处理的新变化"))
			return
		}
		if m.variant == 2 {
			components.Muted(c, tr("支付发布已断开 · 数据截至昨天 17:20"))
		}
		if !m.briefDismissed {
			components.Heading(c, tr("周五答复客户前，退款验收仍在进行"))
			ui.Row(c.Key("sources")).FillWidth().Wrap().Gap(8).Children(func() {
				if m.adaptiveLayout() {
					components.Symbol(c, "doc.text", c.Theme().TextMuted)
					components.Muted(c, tr(m.objects[0].Locator))
					components.Muted(c, tr(m.objects[1].Locator))
				} else {
					if m.taskReference(c.Key("meeting"), 1) {
						m.openNowTask(1)
					}
					if m.taskReference(c.Key("acceptance"), 0) {
						m.openNowTask(0)
					}
				}
			})
			ui.Row(c.Key("actions")).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
				if m.feedback.ID == "feedback-1" && !m.watching {
					if ui.PrimaryButton(c.Key("watch"), tr("关注退款验收")).Clicked() {
						m.setWatching(true)
					}
					if ui.Button(c.Key("dismiss"), tr("忽略这条分析")).Clicked() {
						m.feedback.Status = "dismissed"
						m.briefDismissed = true
					}
				}
				if m.watching {
					components.Muted(c, tr("正在关注"))
				}
			})
		}
		ui.Divider(c.Key("progress-divider"))
		if m.adaptiveLayout() {
			components.Body(c, tr("#40  退款状态说明已更新"))
		} else if components.TextAction(c.Key("open"), tr("#40  退款状态说明已更新")) {
			m.openNowTask(2)
		}
		components.Muted(c, tr("支付发布 · 昨天 17:20"))
	})
}

func (m *mockup) setWatching(watching bool) {
	if m.watching != watching {
		event := "开始关注验收"
		if !watching {
			event = "取消关注验收"
		}
		m.recordTaskEvent(m.mutation.SourceObject, event)
	}
	m.watching = watching
	if m.feedback.ID == "feedback-1" && m.feedback.Status != "dismissed" {
		m.feedback.Status = "open"
		if watching {
			m.feedback.Status = "acted"
		}
	}
}

func (m *mockup) taskReference(c *ui.Context, index int) bool {
	return components.TextAction(c, tr(m.objects[index].Locator))
}

func (m *mockup) nowAcceptance(c *ui.Context) {
	components.ChatReply(c, "Apilot", tr(m.refundConfirmedAt), func() {
		if m.feedback.Status == "dismissed" {
			components.Body(c, tr("退款验收已通过。"))
			return
		}
		components.Heading(c, tr("退款验收通过，客户答复待处理"))
		components.Body(c, tr("已核对原支付渠道的退款记录。"))
		ui.Row(c.Key("actions")).FillWidth().Wrap().Gap(8).Children(func() {
			if !m.adaptiveLayout() && ui.PrimaryButton(c.Key("reply"), tr("查看客户答复任务")).Clicked() {
				m.openNowTask(1)
			}
			if ui.Button(c.Key("dismiss"), tr("忽略这条分析")).Clicked() {
				m.feedback.Status = "dismissed"
			}
		})
	})
}

func (m *mockup) sendNowTurn() {
	if m.framework {
		return
	}
	prompt := strings.TrimSpace(m.nowSession().draft)
	if prompt == "" {
		return
	}
	reply := tr("「验证老订单退款」仍在进行。可以从简报打开验收标准，确认回复客户之前要核对的内容。")
	if m.objects[0].NativeFields["status"] == "已完成" {
		reply = tr("退款验收已通过。你可以查看「回复客户退款结论」里的会议约定，这项任务目前待处理。")
	} else if m.variant == 1 {
		reply = tr("当前演示场景没有需要处理的新变化。")
	}
	if !m.nowSession().briefing {
		reply = tr("可以从任务和连接查看当前状态。此会话中的回复来自本地演示。")
	}
	session := m.nowSession()
	if !session.briefing && len(session.turns) == 0 {
		title := []rune(strings.Join(strings.Fields(prompt), " "))
		session.title = string(title[:min(len(title), 26)])
	}
	session.turns = append(session.turns, nowTurn{prompt: prompt, reply: reply})
	session.lastUsed = time.Now()
	m.nowSession().draft = ""
	m.scrollNowToEnd()
}

func (m *mockup) scrollNowToEnd() {
	// The scrolling element clamps the requested offset to the new content.
	m.nowSession().scroll.ScrollTo(0, math.MaxFloat32, ui.Animation{})
	if m.assistantOpen && m.assistantContext < 0 {
		m.assistantScroll.ScrollTo(0, math.MaxFloat32, ui.Animation{})
	}
}

func (m *mockup) openNowTask(index int) {
	if m.adaptiveLayout() {
		m.assistantOpen = false
		m.navigate(tasksPage)
		m.openFullTask(index)
	} else {
		m.openTask(index)
	}
}

type nowSession struct {
	title    string
	briefing bool
	draft    string
	turns    []nowTurn
	lastUsed time.Time
	scroll   ui.ScrollState
}

func (m *mockup) nowSession() *nowSession {
	return &m.nowSessions[m.nowSessionIndex]
}

func (m *mockup) newNowSession() {
	for _, i := range m.nowSessionOrder() {
		session := &m.nowSessions[i]
		if !session.briefing && len(session.turns) == 0 && strings.TrimSpace(session.draft) == "" {
			m.openNowSession(i)
			m.layout.focusAssistant = true
			return
		}
	}
	m.nowSessions = append(m.nowSessions, nowSession{title: "新会话"})
	if len(m.nowSessions) == nowRecentLimit {
		m.nowSessionsHidden = true
	}
	m.openNowSession(len(m.nowSessions) - 1)
	m.layout.focusAssistant = true
}

func (m *mockup) nowSidebar(c *ui.Context) {
	ui.Row(c.Key("header")).FillWidth().AlignItems(ui.Center).Gap(4).Children(func() {
		disclosure, label := "chevron.down", "收起 Now 会话"
		if m.nowSessionsHidden {
			disclosure, label = "chevron.right", "展开 Now 会话"
		}
		if components.IconButton(c.Key("sessions"), disclosure, tr(label), false).Clicked() {
			m.nowSessionsHidden = !m.nowSessionsHidden
		}
		name := ui.ButtonBase(c.Key("now")).Label("Now").Grow(1).MinHeight(30).Justify(ui.Start).Radius(6)
		name.Children(func() { components.DataText(c, "Now").FontWeight(600) })
		if m.keyboard.focusNavigation && m.page == nowPage {
			name.Focus()
			m.keyboard.focusNavigation = false
		}
		if name.Clicked() {
			m.navigate(nowPage)
		}
		if components.IconButton(c.Key("new"), "plus", tr("新建会话"), false).Clicked() {
			m.newNowSession()
		}
	})
	if !m.nowSessionsHidden {
		ui.Column(c.Key("sessions")).FillWidth().Padding(0, 0, 4, 18).Gap(4).Children(func() {
			order := m.nowSessionOrder()
			for _, index := range order[:min(len(order), nowRecentLimit)] {
				session := m.nowSessions[index]
				if m.nowSidebarLink(c.Key(index), tr(session.title), "bubble", m.page == nowPage && !m.nowLibraryOpen && m.nowSessionIndex == index) {
					m.openNowSession(index)
				}
			}
		})
	}
	if len(m.nowSessions) >= nowRecentLimit && m.nowSidebarLink(c.Key("all"), tr("所有会话"), "square.stack", m.page == nowPage && m.nowLibraryOpen) {
		m.openNowLibrary()
	}
}

const nowRecentLimit = 10

func (m *mockup) openNowLibrary() {
	m.navigate(nowPage)
	m.nowLibraryOpen = true
	m.clearNowReviews()
}

func (m *mockup) openNowSession(index int) {
	m.nowSessionIndex = index
	m.nowSession().lastUsed = time.Now()
	m.navigate(nowPage)
}

func (m *mockup) nowSessionOrder() []int {
	order := make([]int, len(m.nowSessions))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		return m.nowSessions[b].lastUsed.Compare(m.nowSessions[a].lastUsed)
	})
	return order
}

func (m *mockup) nowLibrary(c *ui.Context) {
	search := ui.SearchField(c.Key("query"), &m.nowQuery).Label(tr("搜索会话")).Placeholder(tr("搜索会话")).FillWidth()
	m.keyboard.editingText = search.Focused()
	query := strings.ToLower(strings.TrimSpace(m.nowQuery))
	ui.Scroll(c.Key("results")).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			found := false
			for _, i := range m.nowSessionOrder() {
				session := &m.nowSessions[i]
				title := tr(session.title)
				if query != "" && !strings.Contains(strings.ToLower(title), query) &&
					!slices.ContainsFunc(session.turns, func(turn nowTurn) bool {
						return strings.Contains(strings.ToLower(tr(turn.prompt)), query) || strings.Contains(strings.ToLower(tr(turn.reply)), query)
					}) {
					continue
				}
				found = true
				if m.nowSidebarLink(c.Key(i), title, "bubble", i == m.nowSessionIndex) {
					m.openNowSession(i)
				}
			}
			if !found {
				components.Muted(c.Key("empty"), tr("没有匹配的会话"))
			}
		})
	})
}

func (m *mockup) nowSidebarLink(c *ui.Context, title, icon string, active bool) bool {
	button := ui.ButtonBase(c).Label(title).Tooltip(title).Grow(1).MinWidth(0).MinHeight(30).Radius(6).Padding(4, 6).Justify(ui.Start)
	color := c.Theme().Text
	if active {
		button.Selected(true).Background(c.Theme().Selection)
		color = c.Theme().Accent
	} else if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			components.Symbol(c, icon, c.Theme().TextMuted)
			components.DataText(c, title).TextColor(color).SingleLine().Ellipsis("…").MinWidth(0)
		})
	})
	return button.Clicked()
}
