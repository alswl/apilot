package workbench

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAdaptiveLayoutRetainsWorkspaceWhenResized(t *testing.T) {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	m.useAdaptiveLayout()
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "查看验收依据")
	thread := m.assistantThreadFor(0)
	click(t, p, "询问当前工作")
	p.Type("核对退款渠道")
	selection := m.taskSelection
	if selection != "refund" || m.layout.stacked {
		t.Fatal("expected the brief to select the refund task in a side-by-side workspace")
	}

	p.SetSize(940, 700)
	width := float32(940 - sidebarWidth - sidebarDivider)
	if !m.layout.stacked || m.layout.listWidth != width || m.layout.detailWidth != width {
		t.Fatal("expected both stacked panes to use the width available beside the sidebar")
	}
	list, listVisible := p.Find("每页数量")
	composer, composerVisible := p.Find("询问当前工作")
	if !listVisible || !composerVisible || list.Y >= composer.Y {
		t.Fatalf("expected visible pagination above the task composer, got list=%+v, composer=%+v", list, composer)
	}
	if thread.draft != "核对退款渠道" || m.taskSelection != selection {
		t.Fatalf("resizing should keep the selection and draft, got selection %q draft %q", m.taskSelection, thread.draft)
	}

	click(t, p, "收起侧边栏")
	if !m.layout.sidebarHidden || m.layout.stacked || p.HasText("定时任务") {
		t.Fatal("hiding the sidebar should restore the side-by-side layout when it fits")
	}
	if _, showing := p.Find("工作区布局"); showing {
		t.Fatal("the workspace should arrange itself without a layout picker")
	}
	click(t, p, "显示侧边栏")
	if m.layout.sidebarHidden || !m.layout.stacked || !p.HasText("定时任务") {
		t.Fatal("showing the sidebar should restore navigation and stack the narrower workspace")
	}
	page := m.taskPageIndex
	p.Key(0, ui.KeyEscape)
	if m.inspector || m.taskSelection != selection || m.taskPageIndex != page {
		t.Fatal("sidebar toggles and Escape should keep the selection and page")
	}
	click(t, p, "询问当前工作")
	p.Key(0, ui.KeyEnter)
	if len(thread.turns) != 1 || thread.turns[0].question != "核对退款渠道" || m.assistant != nil {
		t.Fatal("the inline composer should submit the preserved draft without opening a window")
	}
}

func TestAdaptiveSplitDividerChangesPaneSize(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	p := ui.NewTester(m.view, 1190, 730)
	before := m.layout.listWidth
	divider := sidebarWidth + sidebarDivider + before
	p.Press(divider, 325)
	p.Move(divider-100, 325)
	p.Release(divider-100, 325)
	if m.layout.listWidth >= before || m.layout.listWidth < layoutListMin {
		t.Fatalf("dragging should resize the list within its minimum width, got %v from %v", m.layout.listWidth, before)
	}
	p.SetSize(720, 660)
	p.SetSize(1190, 730)
	if m.layout.listWidth >= before {
		t.Fatal("the horizontal split should keep its dragged share after resizing")
	}
}

func TestAdaptiveNowNavigationOpensBriefAndPreservesTasks(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	m.search = "退款"
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "验证老订单退款")
	click(t, p, "Now")
	if m.page != nowPage || !p.HasText("周五答复客户前，退款验收仍在进行") {
		t.Fatal("the sidebar Now action should open the briefing page")
	}

	click(t, p, "追问这份简报")
	p.Type("有哪些验收标准？")
	p.Key(0, ui.KeyEnter)
	if len(m.nowSession().turns) != 1 || m.nowSession().turns[0].prompt != "有哪些验收标准？" {
		t.Fatal("the assistant button on Now should focus the briefing composer")
	}

	click(t, p, "支付改造.md › 验收标准")
	if m.page != nowPage {
		t.Fatal("briefing citations should be visible as plain text")
	}
	p.Key(ui.Cmd, ui.Key2)
	if !m.unifiedTasks() || m.taskSelection != "refund" || m.search != "退款" {
		t.Fatal("returning from Now should keep the task selection and filter")
	}

	click(t, p, "收起侧边栏")
	p.Key(ui.Cmd, ui.Key1)
	if m.page != nowPage || !m.layout.sidebarHidden || !p.HasText("有哪些验收标准？") {
		t.Fatal("Cmd+1 should return to the same briefing with the sidebar hidden")
	}
}
