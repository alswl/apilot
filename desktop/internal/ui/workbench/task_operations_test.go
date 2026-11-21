package workbench

import (
	"slices"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestObsidianEditWritesOnlyAfterSourceConfirmation(t *testing.T) {
	m := newMockup()
	edit := sourceDraft(m.objects[0], obsidianTasks)
	edit.state, edit.labels, edit.due, edit.start = "已完成", []string{"refund", "finance/week"}, "2026-11-23", "2026-11-17"
	var confirm func()
	after := func(_ time.Duration, complete func()) { confirm = complete }
	if problem := m.submitSourceEdit(edit, after); problem != "" {
		t.Fatal(problem)
	}
	if confirm == nil || !m.sourceWritePending("refund") || m.objects[0].NativeFields["checkbox"] != "/" {
		t.Fatal("confirmed task must stay unchanged while its patch is pending")
	}
	m.openTask(1)
	confirm()
	task := m.objects[0]
	if task.NativeFields["checkbox"] != "x" || task.NativeFields["due"] != "2026-11-23" ||
		task.NativeFields["done"] == "" || !slices.Equal(task.Labels, []string{"refund", "finance/week"}) ||
		m.objects[1].NativeFields["checkbox"] != " " {
		t.Fatalf("unexpected source update: %+v", task)
	}
	if task.Activity[len(task.Activity)-1].Text != "来源确认了任务修改" {
		t.Fatal("expected confirmed edit in the activity timeline")
	}
}

func TestGitHubClosedReasonAndRepositoryScopedFields(t *testing.T) {
	m := newMockup()
	edit := sourceDraft(m.objects[2], githubIssues)
	edit.labels = []string{"not-a-repo-label"}
	after := func(_ time.Duration, confirm func()) { confirm() }
	if problem := m.submitSourceEdit(edit, after); problem == "" {
		t.Fatal("unknown repository labels must be rejected")
	}
	edit.labels, edit.assignees, edit.milestone, edit.state = []string{"refund", "bug"}, "lin, chen", "November release", "不计划处理"
	if problem := m.submitSourceEdit(edit, after); problem != "" {
		t.Fatal(problem)
	}
	obj := m.objects[2]
	if !taskClosed(obj) || obj.NativeFields["state_reason"] != "not_planned" ||
		!slices.Equal(obj.Assignees, []string{"lin", "chen"}) || obj.NativeFields["due"] != "" {
		t.Fatalf("unexpected GitHub issue state: %+v", obj)
	}
	edit.state = "开放"
	if problem := m.submitSourceEdit(edit, after); problem != "" {
		t.Fatal(problem)
	}
	if taskClosed(m.objects[2]) || m.objects[2].NativeFields["state_reason"] != "" {
		t.Fatal("reopening should clear the close reason")
	}
}

func TestSourceActionsInTaskDetailsFollowConnector(t *testing.T) {
	for _, tc := range []struct {
		index           int
		present, absent string
	}{{0, "Obsidian 标签", "仓库标签"}, {2, "仓库标签", "Obsidian 标签"}} {
		m := newMockup()
		m.navigate(tasksPage)
		m.openFullTask(tc.index)
		p := ui.NewTester(m.view, 1190, 730)
		p.Key(0, ui.KeyA)
		if m.sourceEditor == nil || !slices.Contains(p.Texts(), tc.present) || slices.Contains(p.Texts(), tc.absent) {
			t.Fatalf("expected source-specific form for object %d", tc.index)
		}
		p.Key(0, ui.KeyEscape)
		if m.sourceEditor != nil || !m.fullTask {
			t.Fatal("Escape should close the editor and keep the current task detail")
		}
	}
}

func TestCreateUsesRepositoryAndIssueNumber(t *testing.T) {
	m := newMockup()
	m.newTaskSource = m.connectionsData[1].Name
	m.createTask("Verify deployment")
	o := m.objects[m.selected]
	if o.NativeID != "41" || o.NativeFields["repo"] != "example/payments" ||
		o.NativeFields["state"] != "open" || o.Locator != "example/payments#41" {
		t.Fatalf("new GitHub issue has wrong address or status: %+v", o)
	}
}

func TestExtendedFixtureSupportsSourceEditingAndCrossLinks(t *testing.T) {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	for _, object := range m.objects {
		conn := m.taskConnection(object)
		if conn == nil {
			t.Fatalf("fixture %s has no source", object.ID)
		}
		edit := sourceDraft(object, conn.SourceKind)
		if problem := m.submitSourceEdit(edit, func(time.Duration, func()) {
			t.Fatalf("unmodified fixture %s should not queue a write", object.ID)
		}); problem != "" {
			t.Fatalf("fixture %s cannot be edited: %s", object.ID, problem)
		}
		for _, relation := range object.Related {
			if relation.TaskID != "" && !slices.ContainsFunc(m.objects, func(o sourceObject) bool {
				return o.ID == relation.TaskID
			}) {
				t.Fatalf("fixture %s links to missing task %s", object.ID, relation.TaskID)
			}
		}
	}
	m.search = "source:github label:=bug -has:assignee"
	rows := m.taskRows()
	if len(rows) != 1 || m.objects[rows[0]].NativeID != "109" {
		t.Fatal("expected the realistic unassigned GitHub retry issue")
	}
}

func TestLiveTaskToggleFollowsConfirmedConnectionCapabilities(t *testing.T) {
	m := newMockup()
	m.live = services.NewRemote("")
	now := time.Now()
	task := sourceObject{ID: "task", ConnectionID: "vault", Kind: common.WorkbenchObsidianTasks, TargetVersion: "observed", State: "open"}
	connection := common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: common.WorkbenchObsidianTasks, WriteEnabled: true}
	snapshot := common.WorkbenchSnapshot{
		WorkbenchConnection: connection, LastSuccess: &now,
		Attempts:     []common.WorkbenchAttempt{{State: "success"}},
		Capabilities: common.WorkbenchCapabilities{Create: true, SetState: true},
	}
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	if !m.canToggleSourceTask(task) || m.editableSource(task) {
		t.Fatal("live task should allow status changes and reject mock editor")
	}
	unknown := task
	unknown.State = "unknown"
	if m.canToggleSourceTask(unknown) {
		t.Fatal("an unknown source status must be mapped before writing")
	}
	snapshot.Attempts[0].State = "failed"
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	if m.canToggleSourceTask(task) || m.objects[0].State != "open" {
		t.Fatal("an offline connection should retain confirmed status and suspend toggles")
	}
	snapshot.Attempts[0].State = "success"
	snapshot.Capabilities = common.WorkbenchCapabilities{Read: true}
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	if m.canToggleSourceTask(task) {
		t.Fatal("read-only connection exposed a status toggle")
	}
}

func TestTaskStatusSymbolsDistinguishSourceStates(t *testing.T) {
	for _, test := range []struct{ state, symbol string }{
		{"open", "square"}, {"in_progress", "square.lefthalf.filled"}, {"done", "checkmark.square.fill"},
		{"cancelled", "minus.square"}, {"unknown", "questionmark.square"},
	} {
		if got := taskStatusSymbol(sourceObject{State: test.state}); got != test.symbol {
			t.Errorf("state %q: got %q, want %q", test.state, got, test.symbol)
		}
	}
}

func TestLiveTaskWriteWaitsForConfirmationAndKeepsStateOnConflict(t *testing.T) {
	m := newMockup()
	m.live = services.NewRemote("")
	snapshot := common.WorkbenchSnapshot{
		WorkbenchConnection: common.WorkbenchConnection{ID: "vault", Kind: obsidianTasks, Name: "Vault"},
		Capabilities:        common.WorkbenchCapabilities{Create: true, SetState: true},
		Attempts:            []common.WorkbenchAttempt{{State: "success"}},
	}
	open := sourceObject{ID: "task", ConnectionID: "vault", Kind: obsidianTasks, State: "open", TargetVersion: "v1"}
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{open})
	m.pendingTaskEdits[open.ID] = true
	m.liveTaskRequests = map[string]common.WorkbenchTaskWrite{open.ID: {RequestID: "complete-once"}}
	m.liveTaskProblems = make(map[string]string)
	for _, state := range []string{common.WorkbenchOperationAccepted, common.WorkbenchOperationDispatching} {
		m.finishLiveTaskToggle(open.ID, common.WorkbenchTaskReceipt{RequestID: "complete-once", SyncState: state}, nil)
		if !m.sourceWritePending(open.ID) || m.liveTaskRequests[open.ID].RequestID != "complete-once" {
			t.Fatalf("%s must retain the pending operation", state)
		}
	}

	confirmed := open
	confirmed.State, confirmed.TargetVersion = "done", "v2"
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{confirmed})
	p := ui.NewTester(func(c *ui.Context) { m.taskStatusCheckbox(c, m.objects[0]) }, 240, 100)
	if !p.HasText("等待来源确认") || taskClosed(m.objects[0]) {
		t.Fatal("a pending write should keep the last confirmed status and show progress")
	}
	m.finishLiveTaskToggle(open.ID, common.WorkbenchTaskReceipt{SyncState: "conflict"}, nil)
	p.Frame()
	if m.sourceWritePending(open.ID) || taskClosed(m.objects[0]) || m.liveTaskRequests[open.ID].RequestID != "" ||
		!p.HasText("来源已变化，请重新核对任务") {
		t.Fatal("a conflict must keep the old status and expose the failure")
	}

	m.pendingTaskEdits[open.ID] = true
	m.liveTaskRequests[open.ID] = common.WorkbenchTaskWrite{RequestID: "check-on-retry"}
	m.finishLiveTaskToggle(open.ID, common.WorkbenchTaskReceipt{SyncState: "needs_review"}, nil)
	if m.liveTaskRequests[open.ID].RequestID != "check-on-retry" || taskClosed(m.objects[0]) {
		t.Fatal("an uncertain write should retain the same request ID and state")
	}
	m.finishLiveTaskToggle(open.ID, common.WorkbenchTaskReceipt{SyncState: "confirmed"}, nil)
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{confirmed})
	if !taskClosed(m.objects[0]) || m.liveTaskProblems[open.ID] != "" {
		t.Fatal("readback of a confirmed write should update the task")
	}
}

func TestLiveTaskSelectionMovesWhenConfirmedTaskLeavesFilter(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	m.page, m.statusFilter = tasksPage, "待处理"
	m.taskSort = ui.SortOrder{Column: "title"}
	m.objects = []sourceObject{
		{ID: "a", Title: "A", State: "open"},
		{ID: "b", Title: "B", State: "open"},
		{ID: "c", Title: "C", State: "open"},
	}
	m.taskSelection = "b"
	updated := slices.Clone(m.objects)
	updated[1].State = "done"
	m.replaceLiveTasks(updated)
	if m.taskSelection != "c" || m.statusFilter != "待处理" || !m.restoreTaskFocus {
		t.Fatalf("selection=%q filter=%q focus=%t", m.taskSelection, m.statusFilter, m.restoreTaskFocus)
	}
}

func TestLiveTaskRecoversOperationFromConnectionSnapshot(t *testing.T) {
	m := newFramework()
	m.live = services.NewRemote("")
	task := sourceObject{ID: "task", ConnectionID: "vault", Kind: obsidianTasks, State: "open", TargetVersion: "v1"}
	snapshot := common.WorkbenchSnapshot{
		WorkbenchConnection: common.WorkbenchConnection{ID: "vault", Name: "Vault", Kind: obsidianTasks, WriteEnabled: true},
		Capabilities:        common.WorkbenchCapabilities{Create: true, SetState: true},
		Attempts:            []common.WorkbenchAttempt{{State: "success"}},
		PendingWrites:       []common.WorkbenchTaskReceipt{{RequestID: "recovered", SourceObjectID: task.ID, SyncState: common.WorkbenchOperationDispatching}},
	}
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	if !m.sourceWritePending(task.ID) {
		t.Fatal("a recovered operation should be shown as pending")
	}
	snapshot.PendingWrites[0].SyncState = common.WorkbenchOperationNeedsReview
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	if m.sourceWritePending(task.ID) {
		t.Fatal("an uncertain operation should not appear to be running")
	}
	p := ui.NewTester(func(c *ui.Context) { m.taskSourceActions(c, &m.objects[0]) }, 400, 160)
	if !p.HasText("核对操作") || !p.HasText("来源尚未确认") {
		t.Fatal("an uncertain recovered operation needs a review action")
	}
	m.toggleSourceTask(task)
	if len(m.liveTaskRequests) != 0 {
		t.Fatal("an uncertain operation must not be replaced by a new write")
	}
	snapshot.PendingWrites = nil
	task.State = "done"
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	if m.sourceWritePending(task.ID) || m.serverTaskOperations[task.ID].RequestID != "" || !taskClosed(m.objects[0]) {
		t.Fatal("a settled operation should leave the confirmed snapshot visible")
	}
}

func TestLiveTaskCreateKeepsDraftUntilConfirmed(t *testing.T) {
	m := newMockup()
	m.newTaskOpen, m.newTaskPending, m.newTaskDraft = true, true, "Write once"
	m.newTaskRequest.RequestID = "create-once"
	count := len(m.objects)
	p := ui.NewTester(m.newTaskDialog, 680, 480)
	if !p.HasText("等待来源确认") || p.HasText("创建任务") {
		t.Fatal("pending create should show progress and hide the submit action")
	}
	m.finishLiveTaskCreate(common.WorkbenchTaskReceipt{SyncState: common.WorkbenchOperationAccepted}, nil)
	if !m.newTaskPending || !m.newTaskOpen {
		t.Fatal("accepted create must wait for source confirmation")
	}
	click(t, p, "后台处理")
	if m.newTaskOpen || !m.newTaskPending {
		t.Fatal("backgrounding should close the dialog without ending the operation")
	}
	m.openNewTask()
	if !m.newTaskOpen || m.newTaskRequest.RequestID != "create-once" {
		t.Fatal("reopening should restore the pending operation")
	}
	m.finishLiveTaskCreate(common.WorkbenchTaskReceipt{SyncState: "needs_review"}, nil)
	if !m.newTaskOpen || m.newTaskPending || m.newTaskDraft != "Write once" || m.newTaskRequest.RequestID != "create-once" || len(m.objects) != count {
		t.Fatal("unconfirmed create should retain the draft and request without adding a row")
	}
	m.finishLiveTaskCreate(common.WorkbenchTaskReceipt{SyncState: "confirmed"}, nil)
	if m.newTaskOpen || m.newTaskPending || m.page != tasksPage {
		t.Fatal("confirmed create should close the form")
	}
}

func TestDevTaskCreatePendingScenario(t *testing.T) {
	m := newDevTaskScenario()
	p := ui.NewTester(m.newTaskDialog, 680, 480)
	if !p.HasText("Write once") || !p.HasText("等待来源确认") {
		t.Fatal("pending form must show the draft and confirmation status")
	}
	click(t, p, "Dev: unconfirmed")
	if m.newTaskPending || !m.newTaskOpen || m.newTaskDraft != "Write once" {
		t.Fatal("unconfirmed result must preserve the draft")
	}
	click(t, p, "创建任务")
	if len(m.devTask.requests) != 2 || m.devTask.requests[1].RequestID != m.devTask.requests[0].RequestID {
		t.Fatalf("retry must reuse the request ID: %+v", m.devTask.requests)
	}
	click(t, p, "Dev: confirm")
	if m.newTaskOpen || m.newTaskPending || len(m.objects) != 1 || m.objects[0].Title != "Write once" {
		t.Fatalf("confirmed readback must close the form and show one task: %+v", m.objects)
	}
}
