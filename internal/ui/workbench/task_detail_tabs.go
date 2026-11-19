package workbench

import (
	"math"
	"time"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

const (
	taskLinksTab = iota
	taskCommentsTab
	taskActivityTab
	taskAssistantTab
)

type taskComment struct {
	text string
	at   string
}

type taskDetailUI struct {
	tab                                     int
	draft                                   string
	labelInput, labelProblem, labelOriginal string
	labelPair, focusLabel                   bool
	comments                                []taskComment
}

func (m *mockup) taskDetailUI(id string) *taskDetailUI {
	if m.taskDetails == nil {
		m.taskDetails = make(map[string]*taskDetailUI)
	}
	if m.taskDetails[id] == nil {
		m.taskDetails[id] = &taskDetailUI{}
	}
	return m.taskDetails[id]
}

func (m *mockup) detailSizeButton(c *ui.Context) {
	icon, label := "arrow.up.left.and.arrow.down.right", "全宽打开任务详情"
	if m.layout.detailExpanded {
		icon, label = "arrow.down.right.and.arrow.up.left", "还原任务详情"
	}
	if components.IconButton(c, icon, tr(label), false).Clicked() {
		m.layout.detailExpanded = !m.layout.detailExpanded
		m.layout.focusDetail = true
		c.Invalidate()
	}
}

func (m *mockup) taskDetailTabs(c *ui.Context, object *sourceObject, detail *taskDetailUI, thread *assistantThread) {
	ui.Column(c.Key("tab-strip")).FillWidth().Children(func() {
		ui.Tabs(c.Key("tabs"), &detail.tab, tr("链接"), tr("评论"), tr("活动"), tr("助理")).FillWidth()
		ui.Divider(c.Key("tab-border"))
	})
	switch detail.tab {
	case taskLinksTab:
		if excerpt := object.NativeFields["excerpt"]; excerpt != "" {
			components.ContentSection(c.Key("source"), tr("来源摘录"), func() {
				components.Muted(c, m.sourceText(object.Locator))
				components.Body(c, tr(excerpt))
			})
		}
		m.taskLinks(c.Key("links"), object)
		if len(object.Related) == 0 && object.NativeFields["excerpt"] == "" {
			components.Muted(c.Key("empty"), tr("暂无链接"))
		}
	case taskCommentsTab:
		if len(detail.comments) == 0 {
			components.Muted(c.Key("empty"), tr("暂无评论"))
		}
		for i, comment := range detail.comments {
			components.AssistantMessage(c.Key(i), tr("我"), comment.at, func() { components.Body(c, comment.text) })
		}
	case taskActivityTab:
		m.taskActivity(c.Key("activity"), object)
		if len(object.Activity) == 0 {
			components.Muted(c.Key("empty"), tr("暂无活动"))
		}
	case taskAssistantTab:
		for i, turn := range thread.turns {
			components.ConversationTurn(c.Key(i), turn.question, turn.reply, nil)
		}
	}
}

func (m *mockup) taskCommentComposer(c *ui.Context, detail *taskDetailUI, scroll *ui.ScrollState) {
	if m.live != nil {
		return
	}
	group := ui.Column(c).FillWidth()
	group.Children(func() {
		if comment := components.Composer(c.Key("input"), &detail.draft, components.ComposerLabels{
			Input: tr("任务评论"), Placeholder: tr("添加评论…"), Send: tr("发表评论"),
		}); comment != "" {
			detail.comments = append(detail.comments, taskComment{text: comment, at: time.Now().Format("15:04")})
			detail.draft = ""
			scroll.ScrollTo(0, math.MaxFloat32, ui.Animation{})
		}
	})
	m.keyboard.editingText = m.keyboard.editingText || group.FocusWithin()
}
