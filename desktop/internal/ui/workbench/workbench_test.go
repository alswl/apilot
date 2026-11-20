package workbench

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func click(t *testing.T, preview *ui.Tester, labels ...string) {
	t.Helper()
	for _, label := range labels {
		if err := preview.Click(label); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNowEvidenceAndSourceProgress(t *testing.T) {
	m := newMockup()
	preview := ui.NewTester(m.view, 1190, 730)
	click(t, preview, "支付改造.md › 验收标准")
	if m.page != nowPage || !m.inspector || m.objects[m.selected].ID != "refund" {
		t.Fatal("evidence must open the refund inspector on Now")
	}
	click(t, preview, "收起任务详情", "关注退款验收")
	if !m.watching || !slices.Contains(preview.Texts(), "正在关注") ||
		slices.Contains(preview.Texts(), "待你判断") {
		t.Fatal("adopting a suggestion must move it out of the decision queue")
	}
	click(t, preview, "原型调试", "模拟来源验收通过", "查看客户答复任务")
	if m.page != nowPage || m.objects[m.selected].ID != "reply" ||
		m.objects[m.selected].NativeFields["status"] != "待处理" {
		t.Fatal("completion must surface the pending customer reply on Now")
	}
}

func TestTaskInspectorPreservesFilteredListOnNarrowWindow(t *testing.T) {
	m := newMockup()
	m.page, m.search, m.sourceFilter = tasksPage, "退款", "工作 vault"
	preview := ui.NewTester(m.view, 860, 580)
	click(t, preview, "验证老订单退款")
	preview.Key(0, ui.KeyEnter)
	click(t, preview, "返回任务")
	if m.inspector || m.page != tasksPage || m.search != "退款" || m.sourceFilter != "工作 vault" || len(m.taskRows()) != 2 {
		t.Fatal("returning from narrow task detail must preserve the filtered task list")
	}
}

func TestDebugConflictAndIgnoredFeedbackAreIndependent(t *testing.T) {
	m := newMockup()
	preview := ui.NewTester(m.view, 1190, 730)
	click(t, preview, "忽略这条分析", "原型调试", "冲突")
	if m.page != tasksPage || m.mutation.Status != "conflict" ||
		m.objects[0].NativeFields["status"] != "进行中" || m.feedback.Status != "dismissed" {
		t.Fatal("choosing a writeback conflict must preserve the source task and feedback")
	}
	click(t, preview, "保留来源值")
	if m.mutation.Status != "confirmed" || m.objects[0].NativeFields["due"] != "11 月 20 日" {
		t.Fatal("keeping the source value must resolve the conflict without writing the draft")
	}
}

func TestNowOpensPreparedAndAcceptsFollowUp(t *testing.T) {
	m := newMockup()
	preview := ui.NewTester(m.view, 1190, 730)
	if !slices.Contains(preview.Texts(), "周五答复客户前，退款验收仍在进行") {
		t.Fatal("Now must show the prepared briefing before any input")
	}
	click(t, preview, "追问这份简报")
	preview.Type("验收进度如何？")
	click(t, preview, "发送追问")
	if m.nowSession().draft != "" || len(m.nowSession().turns) != 1 ||
		m.nowSession().turns[0].prompt != "验收进度如何？" ||
		!slices.Contains(preview.Texts(), m.nowSession().turns[0].reply) {
		t.Fatal("sending must preserve the question and show the reply")
	}
	click(t, preview, "连接", "Now")
	if len(m.nowSession().turns) != 1 || !slices.Contains(preview.Texts(), m.nowSession().turns[0].reply) {
		t.Fatal("switching pages must preserve the conversation")
	}
}

func TestNativeTableSortKeepsSelectedTask(t *testing.T) {
	m := newMockup()
	m.page = tasksPage
	preview := ui.NewTester(m.view, 1190, 730)
	click(t, preview, "验证老订单退款", "截止日", "截止日")
	if m.taskSort.Column != "due" || !m.taskSort.Descending {
		t.Fatalf("expected native header clicks to sort by descending due date, got %+v", m.taskSort)
	}
	if m.inspector || m.taskSelection != "refund" {
		t.Fatal("sorting must preserve the selected task by identity")
	}
}

func TestNarrowTableKeyboardSelectionAndReturn(t *testing.T) {
	m := newMockup()
	m.page = tasksPage
	preview := ui.NewTester(m.view, 860, 580)
	click(t, preview, "回复客户退款结论")
	preview.Key(0, ui.KeyEnter)
	click(t, preview, "返回任务")
	if m.taskSelection != "reply" {
		t.Fatalf("return must restore the selected row, got %q", m.taskSelection)
	}
	preview.Key(0, ui.KeyDown)
	if m.inspector || m.taskSelection != "deploy" {
		t.Fatalf("arrow navigation must move the selection without opening a detail, selected %q", m.taskSelection)
	}
	preview.Key(0, ui.KeyEnter)
	if !m.inspector || m.objects[m.selected].ID != "deploy" {
		t.Fatal("Return must open the highlighted task")
	}
}

func TestTaskDateOrderAndFilterCloseInspector(t *testing.T) {
	m := newMockup()
	m.page, m.taskSort.Column = tasksPage, "due"
	m.objects[0].NativeFields["due"] = "11 月 2 日"
	m.objects[1].NativeFields["due"] = "11 月 10 日"
	m.mutation.Status = "queued"
	if rows := m.taskRows(); !slices.Equal(rows, []int{0, 1, 2}) {
		t.Fatalf("expected chronological order with undated task last, got %v", rows)
	}
	m.taskSort.Descending = true
	if rows := m.taskRows(); !slices.Equal(rows, []int{1, 0, 2}) {
		t.Fatalf("expected reverse chronological order with undated task last, got %v", rows)
	}
	preview := ui.NewTester(m.view, 1190, 730)
	click(t, preview, "验证老订单退款")
	m.sourceFilter = "支付发布"
	preview.Frame()
	if m.inspector || m.taskSelection != "" {
		t.Fatal("changing the filter must remove the hidden task from the inspector and selection")
	}
}

func TestNowReferencesAndInspectorWatch(t *testing.T) {
	m := newMockup()
	preview := ui.NewTester(m.view, 1190, 730)
	click(t, preview, "客户会议.md › 跟进事项")
	if m.objects[m.selected].ID != "reply" {
		t.Fatal("meeting citation must open the customer reply requirements")
	}
	click(t, preview, "收起任务详情", "支付改造.md › 验收标准", "关注验收变化", "收起任务详情")
	if !m.watching || m.feedback.Status != "acted" || !slices.Contains(preview.Texts(), "正在关注") ||
		slices.Contains(preview.Texts(), "关注退款验收") {
		t.Fatal("watching from detail must update the prepared briefing")
	}
}

func TestNarrowPendingWritebackShowsBothValues(t *testing.T) {
	m := newMockup()
	m.page, m.mutation.Status = tasksPage, "queued"
	m.openTask(0)
	preview := ui.NewTester(m.view, 860, 580)
	for _, value := range []string{"来源已确认  11 月 20 日", "拟写入  11 月 19 日"} {
		if !slices.Contains(preview.Texts(), value) {
			t.Fatalf("narrow pending view must include %q", value)
		}
	}
}

func TestAssistantKeepsConversationPerTask(t *testing.T) {
	m := newMockup()
	preview := ui.NewTester(m.assistantView, 600, 410)
	for _, question := range []string{"进展如何", "验收标准是什么"} {
		click(t, preview, "询问当前工作")
		preview.Type(question)
		click(t, preview, "发送")
	}
	m.assistantContext = 1
	other := ui.NewTester(m.assistantView, 600, 410)
	click(t, other, "询问当前工作")
	other.Type("何时答复客户")
	click(t, other, "发送")
	m.assistantContext = 0
	reopened := ui.NewTester(m.assistantView, 600, 410)
	if turns := m.assistantThread().turns; len(turns) != 2 || turns[0].question != "进展如何" ||
		!slices.Contains(reopened.Texts(), "验收标准是什么") {
		t.Fatalf("expected the original task conversation after reopening, got %v", turns)
	}
}

func TestUnknownWritebackRechecksBeforeConfirming(t *testing.T) {
	m := newMockup()
	m.page, m.mutation.Status = tasksPage, "unknown"
	m.openTask(0)
	preview := ui.NewTester(m.view, 860, 580)
	click(t, preview, "核对来源截止日")
	if m.mutation.Status != "conflict" || m.objects[0].NativeFields["due"] != "11 月 20 日" {
		t.Fatal("a proposal that differs from the checked source must stay unresolved")
	}
}

func TestConfirmedDeadlineCanEnterAndCancelEditing(t *testing.T) {
	m := newMockup()
	m.page = tasksPage
	m.openTask(0)
	preview := ui.NewTester(m.view, 860, 580)
	if !slices.Contains(preview.Texts(), "截止日  11 月 20 日") || slices.Contains(preview.Texts(), "保存截止日") {
		t.Fatal("confirmed deadline should start as a read-only value")
	}
	click(t, preview, "编辑截止日")
	if !m.editingDue {
		t.Fatal("editing action must open the deadline editor")
	}
	m.dueDraft = "11 月 23 日"
	preview.Frame()
	click(t, preview, "取消")
	if m.editingDue || m.mutation.Status != "confirmed" || m.objects[0].NativeFields["due"] != "11 月 20 日" {
		t.Fatal("cancel must close the editor without submitting the draft")
	}
	click(t, preview, "编辑截止日")
	if m.dueDraft != "11 月 20 日" {
		t.Fatal("reopening must initialize the editor from the confirmed value")
	}
}

func TestMockAutomationFailureThenRetry(t *testing.T) {
	m := newMockup()
	task := &m.automation.tasks[1]
	var completions []func()
	after := func(_ time.Duration, finish func()) { completions = append(completions, finish) }

	m.automation.startRun(task, after)
	m.automation.startRun(task, after)
	if task.LastRun.Status != "running" || len(completions) != 1 {
		t.Fatal("a running task must block another manual run")
	}
	completions[0]()
	if task.LastRun.Status != "failed" || task.LastRun.Error == "" {
		t.Fatal("first attempt should show the fixture failure and its cause")
	}
	m.automation.startRun(task, after)
	completions[1]()
	if task.LastRun.Status != "succeeded" || task.LastRun.Error != "" || task.Enabled {
		t.Fatal("retry should succeed without enabling scheduled runs")
	}
}

func TestMockConnectionKeepsPreviousConfirmationWhileSyncing(t *testing.T) {
	m := newMockup()
	conn := &m.connectionsData[1]
	conn.Status = "连接断开"
	original := conn.LastConfirmedAt
	var complete func()
	m.syncConnection(conn, func(_ time.Duration, finish func()) { complete = finish })
	if !m.pendingSync[conn.ID] || conn.LastConfirmedAt != original {
		t.Fatal("sync must preserve the last confirmation until completion")
	}
	complete()
	if m.pendingSync[conn.ID] || conn.LastConfirmedAt == original || conn.Status != "同步正常" {
		t.Fatal("reconnect must update connection health and confirmation")
	}
	if newMockup().connectionsData[1].LastConfirmedAt != original {
		t.Fatal("a fresh fixture must retain its original state")
	}
}
