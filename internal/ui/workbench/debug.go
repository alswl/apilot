package workbench

import (
	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) debugPane(c *ui.Context) {
	button := ui.ButtonBase(c.Key("anchor")).Label(tr("原型调试")).Tooltip(tr("原型调试")).Size(32, 30).Radius(6).Center()
	if m.debugOpen || button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() { components.Symbol(c, "slider.horizontal.3", c.Theme().Text) })
	if button.Clicked() {
		m.debugOpen = !m.debugOpen
	}
	ui.Popover(c.Key("pane"), button, &m.debugOpen, func() {
		components.ApplyTheme(c)
		ui.Column(c).Width(290).Padding(12).Gap(10).Children(func() {
			ui.Text(c, tr("原型调试")).Bold()
			ui.Checkbox(c.Key("tags"), &m.showChangeTags, tr("显示 ChangeTag"))
			if ui.Button(c.Key("gallery"), tr("打开组件预览")).Clicked() {
				m.debugOpen = false
				m.gallery.show()
			}
			ui.Divider(c)
			ui.Text(c, tr("Now 场景")).FontSize(12)
			if ui.Tabs(c.Key("now"), &m.variant, tr("已加载"), tr("空结果"), tr("来源过期")).Changed() {
				m.page, m.inspector = nowPage, false
				if m.variant == 2 {
					m.connectionsData[1].Status = "连接断开"
				}
			}
			ui.Text(c, tr("回写场景")).FontSize(12)
			if ui.Tabs(c.Key("sync"), &m.state, tr("冲突"), tr("待回写"), tr("已确认"), tr("待核对")).Changed() {
				m.writeGeneration++
				m.editingDue = false
				m.mutation = loadFixture().Mutation
				m.mutation.Status = []string{"conflict", "queued", "confirmed", "unknown"}[m.state]
				m.objects[0].NativeFields["due"] = m.mutation.BaseFields["due"]
				m.dueDraft = m.mutation.BaseFields["due"]
				m.page = tasksPage
				m.search, m.statusFilter, m.sourceFilter = "", "全部状态", "全部来源"
				m.openTask(0)
				m.debugOpen = false
			}
			ui.Divider(c)
			if ui.Button(c.Key("advance"), tr("模拟来源验收通过")).Disabled(m.objects[0].NativeFields["status"] == "已完成").Clicked() {
				m.completeAcceptance()
			}
		})
	})
}

func (m *mockup) completeAcceptance() {
	m.objects[0].NativeFields["status"] = "已完成"
	m.objects[0].NativeFields["checkbox"] = "x"
	m.objects[0].NativeFields["excerpt"] = "验收记录：老订单退款成功，原支付渠道已核对退款记录。"
	m.objects[1].NativeFields["status"] = "待处理"
	m.recordTaskEvent("refund", "退款验收通过")
	m.recordTaskEvent("reply", "前置验收完成，可以回复客户")
	m.feedback = feedback{
		ID:              "feedback-2",
		EvidenceRefs:    []string{"refund", "reply"},
		SubjectIDs:      []string{"reply"},
		ProposedActions: []string{"查看客户答复任务"},
		Status:          "open",
	}
	m.connectionsData[0].LastConfirmedAt = "刚刚"
	m.refundConfirmedAt = "刚刚"
	m.nowSessionIndex = 0
	m.nowSession().turns = append(m.nowSession().turns, nowTurn{acceptance: true})
	m.scrollNowToEnd()
	m.page, m.variant, m.debugOpen, m.inspector = nowPage, 0, false, false
}
