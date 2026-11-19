package workbench

import (
	"slices"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
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
