package workbench

import (
	"math"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
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
	connectorDetail, connectorHeader                *connectorTaskDetailState
	tab                                             int
	draft                                           string
	labelInput, labelProblem, labelOriginal         string
	labelPair, focusLabel, sourceOpenFailed         bool
	comments                                        []taskComment
	annotations                                     []common.WorkbenchAnnotation
	annotationsLoaded, annotationsLoading           bool
	annotationSaving                                bool
	annotationGeneration                            uint64
	annotationID, annotationBody, annotationProblem string
	localLabelDraft, localLabelProblem              string
	localLabelSaving                                bool
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
		commentTab := "评论"
		if m.live != nil {
			commentTab = "批注"
		}
		ui.Tabs(c.Key("tabs"), &detail.tab, tr("链接"), tr(commentTab), tr("活动"), tr("助理")).FillWidth()
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
		if m.live != nil {
			if !detail.annotationsLoaded && !detail.annotationsLoading {
				m.loadTaskAnnotations(object.ID, detail)
			}
			if detail.annotationsLoading {
				components.Muted(c.Key("loading"), tr("正在读取批注…"))
			}
			if detail.annotationsLoaded && len(detail.annotations) == 0 {
				components.Muted(c.Key("empty"), tr("暂无批注"))
			}
			for _, annotation := range detail.annotations {
				ui.Column(c.Key(annotation.ID)).FillWidth().Gap(4).Children(func() {
					components.AssistantMessage(c.Key("body"), tr("我"), annotation.CreatedAt, func() { components.Body(c, annotation.Body) })
					if components.IconButton(c.Key("remove"), "trash", tr("删除批注"), detail.annotationSaving).Clicked() {
						m.deleteTaskAnnotation(object.ID, detail, annotation.ID)
					}
				})
			}
			if detail.annotationProblem != "" {
				components.Muted(c.Key("error"), detail.annotationProblem)
				if ui.Button(c.Key("retry"), tr("重新读取批注")).Clicked() {
					m.loadTaskAnnotations(object.ID, detail)
				}
			}
			return
		}
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

func (m *mockup) taskCommentComposer(c *ui.Context, object *sourceObject, detail *taskDetailUI, scroll *ui.ScrollState) {
	group := ui.Column(c).FillWidth()
	group.Children(func() {
		if m.live != nil && detail.annotationSaving {
			components.Muted(c.Key("saving"), tr("正在保存批注…"))
			return
		}
		labels := components.ComposerLabels{Input: tr("任务评论"), Placeholder: tr("添加评论…"), Send: tr("发表评论")}
		if m.live != nil {
			labels = components.ComposerLabels{Input: tr("任务批注"), Placeholder: tr("添加批注…"), Send: tr("保存批注")}
		}
		if comment := components.Composer(c.Key("input"), &detail.draft, labels); comment != "" {
			if m.live != nil {
				m.saveTaskAnnotation(object.ID, detail, comment)
				return
			}
			detail.comments = append(detail.comments, taskComment{text: comment, at: time.Now().Format("15:04")})
			detail.draft = ""
			scroll.ScrollTo(0, math.MaxFloat32, ui.Animation{})
		}
	})
	m.keyboard.editingText = m.keyboard.editingText || group.FocusWithin()
}
