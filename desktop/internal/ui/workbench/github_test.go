package workbench

import (
	"slices"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/server"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestGitHubIssueURL(t *testing.T) {
	if got := githubIssueURL("owner/repo", "42"); got != "https://github.com/owner/repo/issues/42" {
		t.Fatal(got)
	}
	for _, pair := range [][2]string{{"other/repo/extra", "1"}, {"../repo", "1"}, {"owner/.", "1"}, {"owner/..", "1"}, {"owner/re\tpo", "1"}, {"owner/re\u00a0po", "1"}, {"owner/repo?x", "1"}, {"owner/repo%2Fother", "1"}, {"owner/repo\\other", "1"}, {"owner/repo", "../1"}, {"owner/repo", "0"}} {
		if got := githubIssueURL(pair[0], pair[1]); got != "" {
			t.Fatalf("accepted invalid target: %q", got)
		}
	}
}

func TestNewTaskRequiresChoiceWhenFilteredSourceIsReadOnly(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{{ID: "readonly", Name: "Read only"}, {ID: "writable", Name: "Writable", Writable: true, Status: "可用"}}
	m.sourceFilter = "Read only"
	count := len(m.objects)
	m.openNewTask()
	if !m.newTaskOpen || m.newTaskSource != "选择来源" {
		t.Fatalf("unexpected default: %q", m.newTaskSource)
	}
	m.createTask("Task")
	if len(m.objects) != count {
		t.Fatal("created task without choosing another source")
	}
	m.newTaskOpen = false
	m.sourceFilter = "Writable"
	m.openNewTask()
	if m.newTaskSource != "Writable" {
		t.Fatalf("writable filter not selected: %q", m.newTaskSource)
	}
}

func TestNewTaskTargetsSelectedSameNameRepository(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{
		{ID: "one", Name: "Shared", Repository: "owner/one", Writable: true, Status: "可用"},
		{ID: "two", Name: "Shared", Repository: "owner/two", Writable: true, Status: "可用"},
	}
	m.sourceFilter, m.sourceFilterID = "Shared", "two"
	m.openNewTask()
	if m.newTaskSource != "Shared · owner/two" {
		t.Fatalf("wrong target label: %q", m.newTaskSource)
	}
	m.createTask("Task")
	if got := m.objects[len(m.objects)-1].ConnectionID; got != "two" {
		t.Fatalf("created in wrong connection: %q", got)
	}
}

func TestNewTaskDoesNotUseReplacementWithSameName(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{{ID: "old", Name: "Shared", Writable: true, Status: "可用"}}
	m.openNewTask()
	m.connectionsData = []connection{{ID: "new", Name: "Shared", Writable: true, Status: "可用"}}
	count := len(m.objects)
	m.createTask("Task")
	if len(m.objects) != count {
		t.Fatal("created a task in replacement connection")
	}
}

func TestGitHubRevocationClearsPendingTaskAndDrafts(t *testing.T) {
	for _, state := range []string{"unavailable", "pending"} {
		t.Run(state, func(t *testing.T) { testGitHubVisibilityClearsPendingTaskAndDrafts(t, state) })
	}
}

func testGitHubVisibilityClearsPendingTaskAndDrafts(t *testing.T, state string) {
	m := newMockup()
	snapshot := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Name: "Repo", Version: 1, AccessState: "available"}}
	task := sourceObject{ID: "private", ConnectionID: "github", Kind: common.WorkbenchGitHubRepository, Title: "Private"}
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	m.pendingTaskEdits[task.ID] = true
	m.liveTaskRequests = map[string]common.WorkbenchTaskWrite{task.ID: {RequestID: "r"}}
	m.taskDetailUI(task.ID).draft = "private draft"
	m.sourceEditor = &taskSourceEdit{id: task.ID, body: "private"}
	m.taskSelection, m.inspector = task.ID, true
	snapshot.AccessState = state
	if state == "pending" {
		snapshot.Version++
		snapshot.AccountLogin = "new-account"
	}
	// Simulate a page response that started before the authorization change.
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
	if len(m.objects) != 0 || m.inspector || m.pendingTaskEdits[task.ID] || m.liveTaskRequests[task.ID].RequestID != "" || m.taskDetails[task.ID] != nil || m.sourceEditor != nil {
		t.Fatalf("protected state survived revocation: %+v", m.objects)
	}
}

func TestGitHubVisibilityClearsUnsubmittedCreateDraft(t *testing.T) {
	for _, target := range []string{"github", "obsidian"} {
		t.Run(target, func(t *testing.T) {
			m := newMockup()
			snapshot := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{
				ID: "github", Kind: common.WorkbenchGitHubRepository, Name: "Repo", Version: 1, AccessState: "available",
			}}
			m.applyLive([]common.WorkbenchSnapshot{snapshot}, nil)
			m.newTaskOpen = true
			m.newTaskSourceID = target
			m.newTaskDraft, m.newTaskBody = "title", "body"
			snapshot.Version++
			snapshot.AccessState = "pending"
			m.applyLive([]common.WorkbenchSnapshot{snapshot}, nil)

			if target == "github" {
				if m.newTaskOpen || m.newTaskDraft != "" || m.newTaskBody != "" {
					t.Fatal("old account's unsubmitted draft survived the account change")
				}
			} else if !m.newTaskOpen || m.newTaskDraft != "title" || m.newTaskBody != "body" {
				t.Fatal("the other connection's draft was cleared")
			}
		})
	}
}

func TestGitHubVisibilityDetectsAuthorizationChanges(t *testing.T) {
	before := []common.WorkbenchSnapshot{{WorkbenchConnection: common.WorkbenchConnection{ID: "repo", Kind: common.WorkbenchGitHubRepository, Version: 1, AccessState: "available", AccountLogin: "one"}}}
	after := append([]common.WorkbenchSnapshot(nil), before...)
	if !sameGitHubVisibility(before, after) {
		t.Fatal("identical visibility considered changed")
	}
	after[0].AccessState = "unavailable"
	if sameGitHubVisibility(before, after) {
		t.Fatal("missed revocation")
	}
	after[0] = before[0]
	after[0].Version++
	if sameGitHubVisibility(before, after) {
		t.Fatal("missed configuration change")
	}
	if sameGitHubVisibility(before, nil) {
		t.Fatal("missed removed connection")
	}
}

func TestGitHubRepositorySearchDisambiguatesSameIssueNumber(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{
		{ID: "one", Name: "One", SourceKind: common.WorkbenchGitHubRepository, SearchFields: []string{"repo", "state_reason"}},
		{ID: "two", Name: "Two", SourceKind: common.WorkbenchGitHubRepository, SearchFields: []string{"repo", "state_reason"}},
	}
	m.objects = []sourceObject{
		{ID: "one-7", ConnectionID: "one", Kind: common.WorkbenchGitHubRepository, NativeID: "7", NativeFields: map[string]string{"repository": "owner/one"}},
		{ID: "two-7", ConnectionID: "two", Kind: common.WorkbenchGitHubRepository, NativeID: "7", NativeFields: map[string]string{"repository": "owner/two"}},
	}
	m.search = "repo:=owner/two id:=#7"
	rows := m.taskRows()
	if len(rows) != 1 || m.objects[rows[0]].ID != "two-7" {
		t.Fatalf("repository filter matched wrong task: %v", rows)
	}
}

func TestGitHubConnectionShowsVerificationAndDuplicateRecovery(t *testing.T) {
	m := newMockup()
	m.connectorEditor = &connectorEdit{connection: connection{SourceKind: common.WorkbenchGitHubRepository}, saving: true}
	p := ui.NewTester(m.connectorDialog, 680, 580)
	if !p.HasText("正在验证连接…") {
		t.Fatal("missing pending connection feedback")
	}
	m.connectorEditor.saving = false
	m.connectorEditor.duplicateID = "existing"
	p.Frame()
	if !p.HasText("查看已有连接") {
		t.Fatal("missing duplicate connection action")
	}
}

func TestGitHubConnectionFieldsInBothLanguagesAndWidths(t *testing.T) {
	previous := englishUI.Load()
	t.Cleanup(func() { englishUI.Store(previous) })
	for _, english := range []bool{false, true} {
		englishUI.Store(english)
		for _, width := range []int{320, 760} {
			m := newMockup()
			m.live = services.NewRemote("")
			edit := &connectorEdit{connection: connection{SourceKind: common.WorkbenchGitHubRepository}, githubRepositories: []string{"owner/repo"}}
			view := ui.NewTester(func(c *ui.Context) { m.githubConnectorFields(c, edit) }, width, 300)
			labels := []string{"选择仓库", "允许回写"}
			if english {
				labels = []string{"Select repository", "Allow writeback"}
			}
			for _, label := range labels {
				if !view.HasText(label) {
					t.Errorf("english=%t width=%d missing %q", english, width, label)
				}
			}
		}
	}
}

func TestGitHubAuthorizationStatesInConnectionForm(t *testing.T) {
	m := newMockup()
	m.live = services.NewRemote("")
	edit := &connectorEdit{connection: connection{SourceKind: common.WorkbenchGitHubRepository}, githubAuthorization: services.GitHubAuthorizationStatus{State: "waiting", UserCode: "ABCD-1234"}}
	view := ui.NewTester(func(c *ui.Context) {
		m.githubConnectorFields(c.Key("fields"), edit)
		m.gitHubConnectionAction(c.Key("action"), edit)
	}, 680, 300)
	if !view.HasText("在 GitHub 输入验证码: ABCD-1234") || !view.HasText("复制验证码并打开 GitHub") || view.HasText("选择 GitHub 授权仓库") || view.HasText("GitHub 令牌") {
		t.Fatal("waiting authorization is not shown correctly")
	}
	edit.githubAuthorization = services.GitHubAuthorizationStatus{State: "authorized", InstallationURL: "https://github.com/apps/example/installations/new"}
	edit.githubNeedsInstall = true
	view.Frame()
	if !view.HasText("GitHub 已授权") || !view.HasText("选择 GitHub 授权仓库") || view.HasText("复制验证码并打开 GitHub") || view.HasText("GitHub 令牌") {
		t.Fatal("authorized state is not shown correctly")
	}
}

func TestLiveTaskDisplaysWorkbenchLabelsAndAnnotations(t *testing.T) {
	m := newMockup()
	m.live = services.NewRemote("")
	object := sourceObject{ID: "task", WorkbenchLabels: []common.WorkbenchLabel{{ID: "label", Name: "follow-up"}}}
	detail := m.taskDetailUI(object.ID)
	detail.tab, detail.annotationsLoaded = taskCommentsTab, true
	detail.annotations = []common.WorkbenchAnnotation{{ID: "note", Body: "local note", CreatedAt: "2026-01-01"}}
	p := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Children(func() {
			m.workbenchLabelProperty(c.Key("labels"), &object)
			m.taskDetailTabs(c.Key("tabs"), &object, detail, &assistantThread{})
		})
	}, 680, 580)
	for _, label := range []string{"工作台标签", "follow-up", "批注", "local note", "删除批注"} {
		if !p.HasText(label) {
			t.Errorf("missing %q in live task metadata", label)
		}
	}
	if !slices.Contains(taskLabelValues(object), "follow-up") {
		t.Fatal("local label is not searchable")
	}
}

func TestLiveTaskMetadataActionsPersistThroughRemote(t *testing.T) {
	dir := t.TempDir()
	runtime, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.Service.ImportWorkbench(common.WorkbenchImport{Tasks: []common.WorkbenchImportTask{{Key: "task", Title: "Task", State: "open"}}})
	if err != nil {
		t.Fatal(err)
	}
	remote := services.NewRemote(config.SocketPath(dir))
	page, err := remote.WorkbenchTasks(common.WorkbenchQuery{Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("task: %+v %v", page, err)
	}
	object := page.Items[0]
	m := newMockup()
	m.live, m.objects = remote, []sourceObject{object}
	detail := m.taskDetailUI(object.ID)
	detail.annotationsLoaded = true
	p := ui.NewTester(func(c *ui.Context) { m.workbenchLabelProperty(c, &object) }, 680, 200)
	click(t, p, "添加工作台标签")
	p.Type("follow-up")
	p.Key(0, ui.KeyEnter)
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		stored, err := remote.WorkbenchTask(object.ID)
		if err == nil && len(stored.WorkbenchLabels) == 1 && stored.WorkbenchLabels[0].Name == "follow-up" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("label was not persisted: %+v %v", stored, err)
		}
	}
	composer := ui.NewTester(func(c *ui.Context) { m.taskCommentComposer(c, &object, detail, &ui.ScrollState{}) }, 680, 100)
	click(t, composer, "任务批注")
	composer.Type("local note")
	composer.Key(0, ui.KeyEnter)
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		annotations, err := remote.WorkbenchAnnotations(object.ID)
		if err == nil && len(annotations) == 1 && annotations[0].Body == "local note" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("annotation was not persisted: %+v %v", annotations, err)
		}
	}
}

func TestGitHubConnectionShowsSharedRateLimit(t *testing.T) {
	m := newMockup()
	until := time.Now().Add(time.Minute)
	c := connection{ID: "github", SourceKind: common.WorkbenchGitHubRepository, Status: "同步正常", RateLimitUntil: &until}
	p := ui.NewTester(func(ctx *ui.Context) { m.syncProgressView(ctx, &c) }, 500, 200)
	if !p.HasText("GitHub 限流，等待至 " + until.Local().Format("15:04:05")) {
		t.Fatal("shared rate-limit wait not shown")
	}
}

func TestGitHubConnectionShowsAuthorizationState(t *testing.T) {
	m := newMockup()
	m.live = services.NewRemote("")
	snapshot := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "repo", Kind: common.WorkbenchGitHubRepository, Name: "Repo", Version: 1, AccessState: "pending"}, Attempts: []common.WorkbenchAttempt{{State: "success"}}}
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, nil)
	if got := m.connectionByID("repo"); got == nil || got.Status != "等待授权确认" || got.Writable {
		t.Fatalf("pending authorization shown incorrectly: %+v", got)
	}
	if m.hasUnavailableTaskSource() {
		t.Fatal("pending authorization shown as failed")
	}
	snapshot.AccessState = "unavailable"
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, nil)
	if got := m.connectionByID("repo"); got == nil || got.Status != "授权不可用" || !m.hasUnavailableTaskSource() {
		t.Fatalf("revoked authorization shown incorrectly: %+v", got)
	}
	snapshot.AccessState, snapshot.WriteDenied = "available", true
	m.applyLive([]common.WorkbenchSnapshot{snapshot}, nil)
	if got := m.connectionByID("repo"); got == nil || got.Capabilities != "只读 · 回写被拒绝" || got.Writable {
		t.Fatalf("write denial shown incorrectly: %+v", got)
	}
}
