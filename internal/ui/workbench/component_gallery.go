package workbench

import (
	"fmt"
	"math"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type componentGallery struct {
	window                   *mygo.Window
	tab                      int
	table                    *mockup
	detail                   *mockup
	rowCount, writebackState int
	selected                 string
	draft                    string
	questions                []string
	scroll                   ui.ScrollState
	ran                      bool
}

func (g *componentGallery) show() {
	if g.window == nil || g.window.IsDestroyed() {
		g.window = mygo.NewWindow(mygo.WindowOptions{
			Title: tr("组件预览 · Apilot"), Width: 840, Height: 540, MinWidth: 660, MinHeight: 420, Content: ui.View(g.view),
		})
	}
	g.window.Show()
	g.window.Focus()
}

func (g *componentGallery) view(c *ui.Context) {
	components.ApplyTheme(c)
	ui.Column(c).Fill().Padding(16).Gap(12).Background(c.Theme().Surface).Children(func() {
		ui.Tabs(c.Key("sections"), &g.tab, tr("控件与状态"), tr("表格"), tr("任务详情"), tr("对话"))
		ui.Divider(c)
		switch g.tab {
		case 0:
			g.controls(c)
		case 1:
			g.dataTable(c)
		case 2:
			g.inspector(c)
		case 3:
			g.conversation(c)
		}
	})
}

func (g *componentGallery) controls(c *ui.Context) {
	cases := []components.ListItem{
		{Title: tr("整理每日笔记"), Subtitle: tr("每天 09:00"), Icon: "clock",
			Status: components.Status{Text: tr("最近运行成功"), Icon: "checkmark.circle", Color: c.Theme().Success}},
		{Title: tr("核对退款、入账和通知记录并归档本周的异常订单"), Subtitle: tr("每 30 分钟"), Icon: "pause.circle",
			Status: components.Status{Text: tr("最近运行失败"), Icon: "exclamationmark.circle", Color: c.Theme().Danger}},
	}
	for _, item := range cases {
		ui.Row(c.Key(item.Title)).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			item.Selected = g.selected == item.Title
			if g.ran && item.Icon == "clock" {
				item.Status.Text = tr("模拟运行成功")
			}
			if components.ListRow(c.Key("item"), item) {
				g.selected = item.Title
			}
			label := tr("运行 ") + item.Title
			if item.Icon == "pause.circle" {
				label += tr("（预览中已禁用）")
			}
			if components.IconButton(c.Key("run"), "play.fill", label, item.Icon == "pause.circle").Clicked() {
				g.ran = true
			}
		})
		ui.Divider(c.Key(item.Title + "-line"))
	}
}

func (g *componentGallery) dataTable(c *ui.Context) {
	if ui.Tabs(c.Key("count"), &g.rowCount, tr("30 行"), tr("1 行"), tr("空表")).Changed() || g.table == nil {
		g.table = newMockup()
		g.table.page = tasksPage
		switch g.rowCount {
		case 0:
			for i := 3; i < 30; i++ {
				object := g.table.objects[i%3]
				object.ID = fmt.Sprintf("preview-%d", i+1)
				object.Title = fmt.Sprintf(tr("订单 %02d · 核对退款、入账和通知记录并归档异常"), i+1)
				g.table.objects = append(g.table.objects, object)
			}
		case 1:
			g.table.objects = g.table.objects[:1]
		case 2:
			g.table.objects = nil
		}
	}
	ui.Scroll(c.Key("table-preview")).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() { g.table.tasks(c.Key("task-table")) })
	})
}

func (g *componentGallery) inspector(c *ui.Context) {
	if ui.Tabs(c.Key("writeback"), &g.writebackState, tr("待确认"), tr("冲突"), tr("待核对")).Changed() || g.detail == nil {
		g.detail = newMockup()
		g.detail.page = tasksPage
		g.detail.openTask(0)
		g.detail.mutation.Status = []string{"queued", "conflict", "unknown"}[g.writebackState]
	}
	g.detail.win = g.window
	if g.detail.inspector {
		g.detail.inspectorPane(c.Key("inspector"), 300)
	} else if ui.Button(c.Key("open"), tr("打开任务详情")).Clicked() {
		g.detail.openTask(0)
	}
}

func (g *componentGallery) conversation(c *ui.Context) {
	ui.Scroll(c.Key("messages")).Grow(1).FillWidth().TrackScroll(&g.scroll).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Children(func() {
			components.AssistantMessage(c.Key("first"), "Apilot", "09:42", func() {
				ui.Text(c, tr("周五答复客户前，需要核对退款验收。"))
			})
			for i, question := range g.questions {
				components.ConversationTurn(c.Key(fmt.Sprintf("turn-%d", i)), question, tr("验收仍在进行。"), func() {
					for _, source := range []string{"客户会议.md › 跟进事项", "支付改造.md › 验收标准"} {
						components.Muted(c.Key(source), tr(source))
					}
				})
			}
		})
	})
	if question := components.Composer(c.Key("composer"), &g.draft, components.ComposerLabels{
		Input: tr("追问"), Placeholder: tr("追问验收进展…"), Send: tr("发送"),
	}); question != "" {
		g.questions = append(g.questions, question)
		g.draft = ""
		g.scroll.ScrollTo(0, math.MaxFloat32, ui.Animation{})
	}
}
