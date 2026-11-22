package workbench

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
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

func TestHostedTaskViewSurvivesWorkspaceRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-views.json")
	open := func() *mockup {
		t.Helper()
		m := newFramework()
		m.connectionsData = []connection{{ID: "work-id", Name: "Work"}}
		if err := m.loadTaskViewStore(path); err != nil {
			t.Fatal(err)
		}
		return m
	}
	first := open()
	first.search, first.sourceFilter, first.taskGrouping = "priority:high", "Work", "按来源分组"
	first.taskSort = ui.SortOrder{Column: "due", Descending: true}
	first.taskPageSizeOverride = 20
	first.taskTable.Columns = ui.TableLayout{Order: []string{"title", "due"}, Widths: map[string]float32{"due": 142}}
	first.taskColumnsHidden["priority"] = false
	view := savedTaskView{ID: "kept", Name: "My work", State: first.viewState()}
	if err := first.saveNamedTaskView(view); err != nil {
		t.Fatal(err)
	}

	second := open()
	if len(second.savedTaskViews) != 1 || second.savedTaskViews[0].Name != view.Name {
		t.Fatalf("saved view missing: %+v", second.savedTaskViews)
	}
	second.selectTaskView(view.ID)
	if second.search != first.search || second.sourceFilter != first.sourceFilter || second.taskGrouping != first.taskGrouping ||
		second.taskSort != first.taskSort || second.taskPageSizeOverride != 20 || second.taskColumnsHidden["priority"] ||
		second.taskTable.Columns.Widths["due"] != 142 || !slices.Equal(second.taskTable.Columns.Order, first.taskTable.Columns.Order) {
		t.Fatal("the reopened view did not restore query, source, sorting, grouping, page size and columns")
	}
	second.search = "priority:low"
	second.saveCurrentTaskView()
	third := open()
	if len(third.savedTaskViews) != 1 || third.savedTaskViews[0].ID != view.ID || third.savedTaskViews[0].State.Query != second.search {
		t.Fatalf("updated view was not persisted in place: %+v", third.savedTaskViews)
	}
}

func TestSavedTaskViewTracksConnectionIDAcrossRenameAndRemoval(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{{ID: "original", Name: "Old name"}}
	m.sourceFilter = "Old name"
	state := m.viewState()
	if state.SourceID != "original" {
		t.Fatalf("source ID was not saved: %+v", state)
	}
	m.connectionsData[0].Name = "New name"
	m.restoreView(state)
	if m.sourceFilter != "New name" {
		t.Fatalf("renamed source not restored: %q", m.sourceFilter)
	}
	m.connectionsData = []connection{{ID: "replacement", Name: "Old name"}}
	m.restoreView(state)
	if !m.taskSourceUnavailable() || len(m.taskRows()) != 0 {
		t.Fatal("removed source selected a replacement")
	}
	if got := m.viewState(); got.SourceID != "original" {
		t.Fatalf("lost unresolved identity: %+v", got)
	}
}

func TestSavedTaskViewFiltersByIDWhenNamesMatch(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{{ID: "one", Name: "Shared"}, {ID: "two", Name: "Shared"}}
	m.objects = []sourceObject{{ID: "first", ConnectionID: "one"}, {ID: "second", ConnectionID: "two"}}
	m.restoreView(taskViewState{Source: "Shared", SourceID: "one", Status: "全部状态"})
	rows := m.taskRows()
	if len(rows) != 1 || m.objects[rows[0]].ID != "first" || m.viewState().SourceID != "one" {
		t.Fatalf("saved source did not preserve exact connection: rows=%v state=%+v", rows, m.viewState())
	}
}

func TestSourceFilterDoesNotSelectSameNameReplacement(t *testing.T) {
	m := newMockup()
	m.sourceFilter, m.sourceFilterID = "Shared", "removed"
	m.connectionsData = []connection{{ID: "replacement", Name: "Shared"}}
	m.objects = []sourceObject{{ID: "task", ConnectionID: "replacement"}}
	if rows := m.taskRows(); len(rows) != 0 {
		t.Fatalf("removed source selected replacement: %v", rows)
	}
}

func TestLiveRenameOfOtherSameNameConnectionPreservesSource(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{{ID: "one", Name: "Shared", SourceKind: common.WorkbenchGitHubRepository}, {ID: "two", Name: "Shared", SourceKind: common.WorkbenchGitHubRepository}}
	m.sourceFilter, m.sourceFilterID = "Shared", "one"
	snapshots := []common.WorkbenchSnapshot{
		{WorkbenchConnection: common.WorkbenchConnection{ID: "one", Name: "Shared", Kind: common.WorkbenchGitHubRepository, AccessState: "available"}},
		{WorkbenchConnection: common.WorkbenchConnection{ID: "two", Name: "Renamed", Kind: common.WorkbenchGitHubRepository, AccessState: "available"}},
	}
	m.applyLive(snapshots, nil)
	if m.sourceFilter != "Shared" || m.sourceFilterID != "one" {
		t.Fatalf("selection changed: %q %q", m.sourceFilter, m.sourceFilterID)
	}
}

func TestLegacyTaskViewRequiresUniqueSourceName(t *testing.T) {
	m := newMockup()
	state := taskViewState{Source: "Shared", Status: "全部状态"}
	m.connectionsData = []connection{{ID: "one", Name: "Shared"}, {ID: "two", Name: "Shared"}}
	m.restoreView(state)
	if !m.taskSourceUnavailable() || len(m.taskRows()) != 0 {
		t.Fatal("ambiguous legacy source selected connections")
	}
	m.connectionsData = m.connectionsData[:1]
	m.restoreView(state)
	if m.sourceFilter != "Shared" || m.viewState().SourceID != "one" {
		t.Fatal("unique legacy source did not resolve")
	}
}

func TestTaskColumnsOpensFromTableHeader(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	m.navigate(tasksPage)
	p := ui.NewTester(m.view, 680, 580)
	if err := p.RightClick("状态"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(p.Menu(), "优先级") || !slices.Contains(p.Menu(), "路径") || !slices.Contains(p.Menu(), "章节") {
		t.Fatalf("header menu does not list columns: %v", p.Menu())
	}
	if err := p.ChooseMenuItem("优先级"); err != nil {
		t.Fatal(err)
	}
	if !m.taskColumnVisible("priority") {
		t.Fatal("choosing a column should change its visibility")
	}
	if err := p.RightClick("状态"); err != nil {
		t.Fatal(err)
	}
	if err := p.ChooseMenuItem("路径"); err != nil {
		t.Fatal(err)
	}
	if err := p.RightClick("状态"); err != nil {
		t.Fatal(err)
	}
	if err := p.ChooseMenuItem("章节"); err != nil {
		t.Fatal(err)
	}
	_, indexes := m.visibleColumns()
	values := cellsAt(m.taskValues(m.objects[0]), indexes)
	if !slices.Contains(values, "Payments/Refunds.md") || !slices.Contains(values, "Acceptance") {
		t.Fatalf("missing source fields: %v", values)
	}
}

func TestSavedViewSplitButtonUpdatesOrSavesCopy(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	view := savedTaskView{ID: "original", Name: "My tasks", State: m.viewState()}
	if err := m.saveNamedTaskView(view); err != nil {
		t.Fatal(err)
	}
	m.selectTaskView(view.ID)
	m.search = "priority:high"
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "更新视图")
	if m.saveTaskViewOpen || len(m.savedTaskViews) != 1 || m.savedTaskViews[0].State.Query != m.search {
		t.Fatal("updating should replace the current saved view in place")
	}
	m.search = "priority:low"
	p.Frame()
	click(t, p, "视图保存选项")
	if err := p.ChooseMenuItem("另存为新视图…"); err != nil {
		t.Fatal(err)
	}
	p.Type("Another view")
	p.Key(0, ui.KeyEnter)
	if len(m.savedTaskViews) != 2 || m.savedTaskViews[0].ID != view.ID || m.taskView == view.ID {
		t.Fatal("saving as new should preserve the original and select the new view")
	}
}

func TestBuiltInStatusViewsFilterTasks(t *testing.T) {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	for id, status := range map[string]string{"in-progress": "进行中", "pending": "待处理"} {
		m.selectTaskView(id)
		rows := m.taskRows()
		if len(rows) == 0 || m.statusFilter != status {
			t.Fatalf("view %s did not select status %s", id, status)
		}
		for _, index := range rows {
			if m.taskStateLabel(m.objects[index]) != tr(status) {
				t.Fatalf("view %s included a task with another status", id)
			}
		}
	}
}

func TestTaskViewSaveTracksColumnChanges(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	m.saveCurrentTaskView()
	if m.saveTaskViewOpen || m.taskViewChanged() {
		t.Fatal("an unchanged view should not be saveable")
	}
	p := ui.NewTester(m.view, 1190, 730)
	if err := p.RightClick("状态"); err != nil {
		t.Fatal(err)
	}
	if err := p.ChooseMenuItem("文件名"); err != nil {
		t.Fatal(err)
	}
	if !m.taskViewChanged() || m.taskColumnVisible("file") {
		t.Fatal("changing File Name visibility should make the view saveable")
	}
	if err := p.RightClick("状态"); err != nil {
		t.Fatal(err)
	}
	if err := p.ChooseMenuItem("文件名"); err != nil {
		t.Fatal(err)
	}
	if m.taskViewChanged() || !m.taskColumnVisible("file") {
		t.Fatal("restoring the original visibility should clear the unsaved change")
	}
	if err := p.RightClick("状态"); err != nil {
		t.Fatal(err)
	}
	if err := p.ChooseMenuItem("文件名"); err != nil {
		t.Fatal(err)
	}
	click(t, p, "保存视图")
	p.Type("File preferences")
	p.Key(0, ui.KeyEnter)
	if m.taskViewChanged() || len(m.savedTaskViews) != 1 {
		t.Fatal("the saved column configuration should be clean")
	}
	m.taskTable.Columns = ui.TableLayout{Order: []string{"source", "title"}, Widths: map[string]float32{"source": 80}}
	if !m.taskViewChanged() {
		t.Fatal("column order and width should count as view changes")
	}
	m.saveCurrentTaskView()
	m.selectTaskView("watching")
	m.selectTaskView(m.savedTaskViews[0].ID)
	if m.taskViewChanged() || m.taskTable.Columns.Widths["source"] != 80 || !slices.Equal(m.taskTable.Columns.Order, []string{"source", "title"}) {
		t.Fatal("the saved view should restore column order and width")
	}
}
