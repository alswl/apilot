package workbench

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFullTaskOpensFromPreviewAndReturnsToView(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	m.search = "#refund -#customer"
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "验证老订单退款")
	if m.inspector || !slices.Contains(p.Texts(), "快速预览") {
		t.Fatal("single click should preview the selected task")
	}
	p.Key(0, ui.KeyEnter)
	if !m.fullTask || !slices.Contains(p.Texts(), "相关资料与产出") || !slices.Contains(p.Texts(), "活动") {
		t.Fatal("Enter should open full details including relations and activity")
	}
	click(t, p, "退款流水核对")
	if !m.taskResourceOpen {
		t.Fatal("the Agent session should open as a related resource")
	}
	p.Key(0, ui.KeyEscape)
	if m.taskResourceOpen || !m.fullTask {
		t.Fatal("Escape should first close the resource")
	}
	click(t, p, "回复客户退款结论")
	if m.objects[m.selected].ID != "reply" || m.taskSelection != "refund" {
		t.Fatal("opening a linked task excluded by the view should preserve the underlying selection")
	}
	p.Key(0, ui.KeyEscape)
	if !m.fullTask || m.objects[m.selected].ID != "refund" || m.taskSelection != "refund" {
		t.Fatal("Escape should return to the task that opened the related task")
	}
	p.Key(0, ui.KeyEscape)
	if m.fullTask || m.inspector || m.taskSelection != "refund" || m.search != "#refund -#customer" {
		t.Fatal("a second Escape should return to the underlying view and selection")
	}
}

func TestRelatedTaskBackLinkRestoresSourcePage(t *testing.T) {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	m.useAdaptiveLayout()
	m.openFullTask(0)
	p := ui.NewTester(m.view, 1190, 730)
	page := m.taskPageIndex
	p.Scroll(1100, 480, 0, 360)
	scrolled := m.assistantThreadFor(0).scroll.Y
	click(t, p, "回复客户退款结论")
	if m.taskPageIndex == page || m.taskSelection != "reply" {
		t.Fatal("the related task should be on a different page")
	}
	click(t, p, "返回 验证老订单退款")
	if m.taskSelection != "refund" || m.taskPageIndex != page || m.currentTaskIndex() != 0 || m.assistantThreadFor(0).scroll.Y != scrolled {
		t.Fatalf("back link restored selection=%q page=%d task=%d scroll=%.1f, want refund page=%d task=0 scroll=%.1f", m.taskSelection, m.taskPageIndex, m.currentTaskIndex(), m.assistantThreadFor(0).scroll.Y, page, scrolled)
	}
}

func TestSaveTaskViewKeepsQueryGroupingAndColumns(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	m.search, m.taskGrouping = "#refund -#customer", "按来源分组"
	m.taskColumnsHidden["labels"] = false
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "保存视图")
	p.Type("退款跟进")
	p.Key(0, ui.KeyEnter)
	if m.saveTaskViewOpen || len(m.savedTaskViews) != 1 || m.taskView == "" {
		t.Fatal("saving should create and open a named view")
	}
	click(t, p, "关注中")
	m.setTaskWatched("reply", true)
	p.Frame()
	if !slices.Equal(m.taskRows(), []int{1}) {
		t.Fatal("the Watching view should include a watched task from any source")
	}
	click(t, p, "退款跟进")
	if m.search != "#refund -#customer" || m.taskGrouping != "按来源分组" || m.taskColumnsHidden["labels"] || m.taskWatchOnly {
		t.Fatal("returning to a saved view should restore its query, grouping and visible columns")
	}
	if !slices.Contains(p.Texts(), "工作 vault") || !slices.Contains(p.Texts(), "支付发布") {
		t.Fatal("grouped view should expose both source group headings")
	}
}
