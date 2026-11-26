package workbench

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestTaskBoardUsesAllMatchingTasksAndPreservesView(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	m.live, m.liveLoaded = &services.Remote{}, true
	m.layout.detailHidden = true
	m.connectionsData = []connection{{ID: "one", Name: "QA Board Alpha"}, {ID: "two", Name: "QA Board Beta"}}
	m.objects = nil
	for _, conn := range m.connectionsData {
		for i := range 12 {
			priority := 5 - i%5
			m.objects = append(m.objects, sourceObject{
				ID: fmt.Sprintf("%s-%02d", conn.ID, i), ConnectionID: conn.ID, Kind: common.WorkbenchLocal,
				NativeID: fmt.Sprintf("task-%02d", i), Title: fmt.Sprintf("Board %02d", i),
				State: taskBoardStates[i%len(taskBoardStates)], DueDate: fmt.Sprintf("2027-03-%02d", 24-i), Priority: &priority,
			})
		}
	}
	m.taskPageSizeOverride, m.taskPageSize = 10, 10
	m.search = "Board"
	m.taskSort = ui.SortOrder{Column: "due"}
	m.taskGrouping = "按来源分组"
	p := ui.NewTester(m.view, 880, 680)
	list := m.taskRows()
	if len(list) != 24 || len(m.pagedTaskRows(list)) != 10 {
		t.Fatal("fixture should exceed one displayed page")
	}
	click(t, p, "看板")
	if !m.taskBoard || !p.HasText("共 24 项") || m.search != "Board" || m.taskView != "" {
		t.Fatalf("board switch changed view or query: %+v", m.viewState())
	}
	rows := m.taskRows()
	if !slices.Equal(slices.Sorted(slices.Values(list)), slices.Sorted(slices.Values(rows))) {
		t.Fatalf("board and list membership differ: %v and %v", rows, list)
	}
	for i, column := range m.taskBoardColumns(rows) {
		want := 4
		if i < 2 {
			want = 6
		}
		if len(column) != want || !slices.IsSortedFunc(column, func(a, b int) int {
			return parseTaskDate(m.objects[a].DueDate).Compare(parseTaskDate(m.objects[b].DueDate))
		}) {
			t.Fatalf("state %s count or due order: %v", taskBoardStates[i], column)
		}
	}
	for m.taskBoardScroll.X < m.taskBoardScroll.MaxX {
		previous := m.taskBoardScroll.X
		click(t, p, "向右浏览看板")
		if m.taskBoardScroll.X <= previous {
			t.Fatal("scroll right did not advance")
		}
	}
	x := m.taskBoardScroll.X
	if x <= 0 {
		t.Fatal("narrow board has no horizontal travel")
	}
	last := m.objects[m.taskBoardColumns(rows)[4][0]]
	click(t, p, fmt.Sprintf("%s · %s · 优先级 %d · 截止日 %s", last.Title, m.taskSourceDescription(last), *last.Priority, last.DueDate))
	if !m.layout.detailExpanded || m.currentTaskIndex() < 0 || m.objects[m.currentTaskIndex()].ID != last.ID {
		t.Fatal("card did not open the exact task")
	}
	p.Key(0, ui.KeyEscape)
	if m.layout.detailExpanded || m.taskBoardScroll.X != x || m.search != "Board" || m.taskSelection != last.ID {
		t.Fatalf("return lost board context: x=%v selection=%q", m.taskBoardScroll.X, m.taskSelection)
	}
	click(t, p, "QA Board Alpha", "全部状态", "进行中")
	if !p.HasText("共 3 项") {
		t.Fatalf("source and state counts should intersect: %v", p.Texts())
	}
	click(t, p, "清除来源条件")
	if m.statusFilter != "进行中" || m.search != "Board" || !p.HasText("共 6 项") {
		t.Fatal("clearing source changed another condition")
	}
	click(t, p, "清除状态条件")
	if m.statusFilter != "全部状态" || !p.HasText("共 24 项") {
		t.Fatal("clearing status did not restore count")
	}
	m.search = "no match"
	p.Frame()
	if !p.HasText("共 0 项") || !p.HasText("没有符合条件的任务") {
		t.Fatal("zero board matches should show count and empty state")
	}
	click(t, p, "搜索任务")
	if !p.Focused("搜索任务") {
		t.Fatal("search is unavailable at zero results")
	}
	click(t, p, "列表")
	if m.taskBoard || m.search != "no match" || m.taskGrouping != "按来源分组" || m.taskPageSizeOverride != 10 {
		t.Fatal("return to list lost state or list configuration")
	}
}

func TestTaskBoardSavedLayoutAndObsidianPriority(t *testing.T) {
	m := newMockup()
	m.useAdaptiveLayout()
	m.connectionsData = []connection{{ID: "vault", Name: "Board vault", SourceKind: obsidianTasks, SearchFields: []string{"file", "priority"}}}
	high, low := 5, 2
	m.objects = []sourceObject{
		{ID: "high", Title: "Review note", Kind: obsidianTasks, ConnectionID: "vault", State: "open", Priority: &high,
			DueDate: "2027-03-20", NativeFields: map[string]string{"priority": "high", "due": "2027-03-20"}, Labels: []string{"review"}},
		{ID: "low", Title: "Draft note", Kind: obsidianTasks, ConnectionID: "vault", State: "open", Priority: &low,
			NativeFields: map[string]string{"priority": "low"}, Labels: []string{"review"}},
	}
	m.sourceFilter, m.sourceFilterID = "Board vault", "vault"
	m.taskBoard = true
	m.taskSort = ui.SortOrder{Column: "priority"}
	for _, query := range []string{"priority:high #review", "priority:=\"high\""} {
		m.search = query
		if got := m.taskRows(); !slices.Equal(got, []int{0}) {
			t.Fatalf("native priority query %s matched %v", query, got)
		}
	}
	if values := m.searchValues(m.objects[0], "priority"); !slices.Equal(values, []string{"high"}) {
		t.Fatalf("quick filter values: %v", values)
	}
	m.search = ""
	if got := m.taskBoardColumns(m.taskRows())[0]; !slices.Equal(got, []int{1, 0}) {
		t.Fatalf("priority sort did not use common numeric order: %v", got)
	}
	draft := sourceDraft(m.objects[0], obsidianTasks)
	preview := ui.NewTester(func(c *ui.Context) { obsidianPriorityChoice(c, &draft.priority) }, 320, 280)
	if draft.priority != "high" || !preview.HasText("高") {
		t.Fatalf("source editor lost native priority: %q, %v", draft.priority, preview.Texts())
	}
	click(t, preview, "高", "低")
	if draft.priority != "low" {
		t.Fatalf("source editor should keep a native priority value, got %q", draft.priority)
	}
	p := ui.NewTester(m.view, 1190, 730)
	if !p.HasText("Review note") || !p.HasText("Board vault") || !p.HasText("优先级 高") || !p.HasText("2027-03-20") {
		t.Fatalf("card metadata missing: %v", p.Texts())
	}
	m.taskGrouping = "按来源分组"
	state := m.viewState()
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var restored taskViewState
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	m.restoreView(defaultTaskViewState(""))
	if m.taskBoard {
		t.Fatal("old and default views should open as lists")
	}
	m.restoreView(restored)
	if !m.taskBoard || m.taskGrouping != state.Grouping || m.taskSort != state.Sort || m.sourceFilterID != state.SourceID {
		t.Fatalf("saved view round trip: %+v", m.viewState())
	}
}
