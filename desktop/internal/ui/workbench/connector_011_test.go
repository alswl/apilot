package workbench

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

func connectorUIFixture() *mockup {
	m := newFramework()
	m.useAdaptiveLayout()
	m.hosted = true
	m.live = services.NewRemote("/invalid/apilot-test.sock")
	m.livePlugins = []services.WorkbenchPlugin{{ID: common.WorkbenchGitHubRepository, Enabled: true, Release: &common.WorkbenchConnectorRelease{ConnectorID: managedGitHub, ReleaseVersion: "0.1.0"}}}
	now := time.Date(2026, 11, 23, 9, 30, 0, 0, time.Local)
	makeScope := func(id, name, status string) common.WorkbenchConnectorScopeOverview {
		return common.WorkbenchConnectorScopeOverview{WorkbenchScopeState: common.WorkbenchScopeState{ScopeID: id, ConnectionID: "parent", AccessState: "available", LastSuccess: &now},
			Label: name, LastRun: &common.WorkbenchSyncRun{ID: "run-" + id, ScopeID: id, ConnectionID: "parent", ConnectorID: managedGitHub, Mode: "full", Status: status,
				StartedAt: now, Requests: 2, Pages: 1, Fetched: 2, Changed: 2, UsageComplete: status == "success"}}
	}
	m.connectorParents = []common.WorkbenchConnectorOverview{{Connection: common.WorkbenchConnectorConnection{ID: "parent", ConnectorID: managedGitHub, EndpointRef: "github.com", AuthState: "authorized"},
		Scopes: []common.WorkbenchConnectorScopeOverview{makeScope("pay", "example/pay", "success"), makeScope("ops", "example/ops", "failed")}}}
	m.expandedConnectors = map[string]bool{"parent": true}
	priority := 2
	m.applyLive([]common.WorkbenchSnapshot{
		{WorkbenchConnection: common.WorkbenchConnection{ID: "local", Kind: common.WorkbenchLocal, Name: "Local Tasks"}, Capabilities: common.WorkbenchCapabilities{Create: true}},
		{ParentConnectionID: "parent", ScopeRef: "123", WorkbenchConnection: common.WorkbenchConnection{ID: "pay", Kind: managedGitHub, Name: "example/pay", WriteEnabled: true, AccountLogin: "alex", AccessState: "available", Version: 1}, Capabilities: common.WorkbenchCapabilities{Create: true}},
		{ParentConnectionID: "parent", ScopeRef: "456", WorkbenchConnection: common.WorkbenchConnection{ID: "ops", Kind: managedGitHub, Name: "example/ops", AccountLogin: "alex", AccessState: "available", Version: 1}},
		{WorkbenchConnection: common.WorkbenchConnection{ID: "obsidian", Kind: common.WorkbenchObsidianTasks, Name: "Hidden vault"}},
		{ParentConnectionID: "fixture-parent", WorkbenchConnection: common.WorkbenchConnection{ID: "fixture", Kind: managedFixture, Name: "Hidden fixture", AccessState: "available"}},
	}, []sourceObject{
		{ID: "pay1", Kind: managedGitHub, ConnectionID: "pay", NativeID: "42", Title: "Confirm payment exports", State: "open", DueDate: "2026-12-15", Priority: &priority, Assignees: []string{"Alice"}, TargetVersion: "v1"},
		{ID: "ops1", Kind: managedGitHub, ConnectionID: "ops", NativeID: "42", Title: "Check deployment window", State: "open", TargetVersion: "v1"},
		{ID: "local1", Kind: common.WorkbenchLocal, ConnectionID: "local", Title: "Prepare review", State: "open", TargetVersion: "v1"},
		{ID: "hidden1", Kind: common.WorkbenchObsidianTasks, ConnectionID: "obsidian", Title: "Hidden vault task"},
		{ID: "hidden2", Kind: managedFixture, ConnectionID: "fixture", Title: "Hidden fixture task"},
	})
	return m
}

func TestConnector011DesktopScopeNavigationAndVisibility(t *testing.T) {
	previous := englishUI.Swap(false)
	t.Cleanup(func() { englishUI.Store(previous) })
	m := connectorUIFixture()
	if len(m.connectionsData) != 3 || len(m.objects) != 3 {
		t.Fatalf("production sources: %d / %d", len(m.connectionsData), len(m.objects))
	}
	if allowLocalConnectorFixture("/tmp/Apilot.app/Contents/MacOS/Apilot", "/tmp/fixture.json") ||
		!allowLocalConnectorFixture("/tmp/apilot-app", "/tmp/fixture.json") ||
		allowLocalConnectorFixture("/tmp/apilot-app", "") {
		t.Fatal("the test Connector escaped the standalone development boundary")
	}
	if m.visibleConnectorKind("jira") {
		t.Fatal("Jira was offered")
	}
	m.page = connectionsPage
	view := ui.NewTester(m.view, 1190, 730)
	if !view.HasText("alex") || !view.HasText("example/pay") || !view.HasText("example/ops") || view.HasText("Hidden fixture") {
		t.Fatalf("scope list: %v", view.Texts())
	}
	click(t, view, "查看任务")
	if m.page != tasksPage || m.sourceFilterID != "pay" || len(m.taskRows()) != 1 || m.objects[m.taskRows()[0]].ID != "pay1" {
		t.Fatalf("scope task route: %s %+v", m.sourceFilterID, m.taskRows())
	}
	m.search = "assignee:Alice"
	if len(m.taskRows()) != 1 {
		t.Fatal("projected assignee filter lost the task")
	}
}

func TestConnector011DesktopPanelsAtAdaptiveSizes(t *testing.T) {
	for _, size := range [][2]int{{1190, 730}, {860, 580}, {680, 580}} {
		for _, en := range []bool{false, true} {
			old := englishUI.Swap(en)
			m := connectorUIFixture()
			m.page = connectionsPage
			m.connectorLogin = &connectorLoginPanel{kind: managedGitHub, auth: common.WorkbenchConnectorAuthorization{ID: "auth", Status: "authorized", DisplayName: "alex"},
				choices: []protocol.ConfigOption{{Value: "123", Label: "example/pay"}, {Value: "456", Label: "example/ops"}}, selected: map[string]bool{}}
			view := ui.NewTester(m.view, size[0], size[1])
			if err := view.Click("example/pay"); err != nil {
				t.Error(err)
			}
			if !m.connectorLogin.selected["123"] || !view.HasText(tr("保存所选范围")) {
				t.Error("scope selection did not remain visible")
			}
			view.Key(0, ui.KeyEscape)
			if m.connectorLogin != nil {
				t.Error("Escape did not close login")
			}
			m.connectorMapping = &connectorMappingPanel{scope: "pay", task: "pay1", draft: map[string]protocol.MappingRule{"due_date": {Field: "due_date"}},
				catalog: protocol.FieldCatalog{Fields: []protocol.FieldDefinition{{ID: "due_date", Label: "Due date", Type: protocol.FieldDate, Writable: true}, {ID: "promised", Label: "Promised", Type: protocol.FieldDate}}},
				value:   common.WorkbenchMappingResult{Sample: &m.objects[0]}}
			view = ui.NewTester(m.view, size[0], size[1])
			if !view.HasText(tr("字段映射")) || !view.HasText(tr("预览")) {
				t.Error("mapping controls unavailable")
			}
			view.Key(0, ui.KeyEscape)
			if m.connectorMapping != nil {
				t.Error("Escape did not close mapping")
			}
			englishUI.Store(old)
		}
	}
}

func TestConnector011DesktopDetailOperationAndUsagePresentation(t *testing.T) {
	old := englishUI.Swap(false)
	t.Cleanup(func() { englishUI.Store(old) })
	m := connectorUIFixture()
	task := m.objects[0]
	detail := m.taskDetailUI(task.ID)
	detail.connectorDetail = &connectorTaskDetailState{version: task.TargetVersion, segment: "description", loaded: true,
		page: common.WorkbenchTaskDetail{Cached: true, Items: []protocol.DetailItem{{ID: "description", Text: &protocol.RichText{Format: "markdown", Content: protocol.Value(protocol.FieldText, "Review **export results**").Value}}}}}
	view := ui.NewTester(func(c *ui.Context) { m.managedTaskDetail(c, &task, detail) }, 680, 580)
	if !view.HasText("已缓存") {
		t.Fatal("detail did not render")
	}
	if !view.HasText("Review **export results**") {
		t.Fatalf("description missing: %v", view.Texts())
	}

	m.connectorTaskEditor = &connectorTaskEditor{task: task, actionMode: true, action: "resolve",
		actions:      []protocol.StateAction{{ID: "resolve", Label: "Resolve", Required: []protocol.FieldDefinition{{ID: "resolution", Label: "Resolution", Type: protocol.FieldText}}}},
		actionValues: map[string]string{}}
	view = ui.NewTester(m.connectorTaskEditorDialog, 680, 580)
	click(t, view, "提交")
	if !view.HasText("填写操作所需字段 · 解决结果") || m.sourceWritePending(task.ID) {
		t.Fatal("missing transition input was dispatched")
	}
	view.Key(0, ui.KeyEscape)
	if m.connectorTaskEditor != nil {
		t.Fatal("Escape did not close the task editor")
	}

	m.connectorUsage = connectorUsageState{loaded: true, items: []common.WorkbenchSyncUsage{{ConnectorID: managedGitHub, ConnectionID: "parent", ScopeID: "ops", Mode: "delta", LatestSyncRunID: "run-ops", Runs: 3, Success: 2, Failed: 1, NoChanges: 2, Requests: 5, RateLimits: 1, PartialRuns: 1}}}
	view = ui.NewTester(m.managedSyncUsage, 680, 580)
	if !view.HasText("最近一轮") || !view.HasText("部分计量") || !view.HasText("3 次同步 · 2 成功 · 1 失败 · 2 无变化") {
		t.Fatalf("usage: %v", view.Texts())
	}
	run := *m.connectorParents[0].Scopes[1].LastRun
	run.ErrorKind, run.FailedStage = "rate_limited", "read"
	retry := time.Now().Add(time.Hour)
	run.RetryAt = &retry
	m.connectorRuns = &connectorRunPanel{detail: &run}
	view = ui.NewTester(m.connectorRunsDialog, 680, 580)
	if !view.HasText("部分计量") || !view.HasText("立即同步") || !view.HasText("全量同步 · "+run.StartedAt.Local().Format(time.DateTime)+" · "+run.ID) {
		t.Fatalf("run detail: %v", view.Texts())
	}
	view.Key(0, ui.KeyEscape)
	if m.connectorRuns != nil {
		t.Fatal("log did not return to the prior surface")
	}
}

func TestConnector011Evidence(t *testing.T) {
	dir := os.Getenv("APILOT_TEST_UI_EVIDENCE_DIR")
	if dir == "" {
		t.Skip("set APILOT_TEST_UI_EVIDENCE_DIR to export UI evidence")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := englishUI.Load()
	t.Cleanup(func() { englishUI.Store(old) })
	specimens := []struct {
		name          string
		width, height int
		en            bool
		view          func(*mockup) *ui.Tester
	}{
		{"uc01-connections-en", 1190, 730, true, func(m *mockup) *ui.Tester { m.page = connectionsPage; return ui.NewTester(m.view, 1190, 730) }},
		{"uc01-multi-scope-zh", 680, 580, false, func(m *mockup) *ui.Tester {
			m.page = connectionsPage
			m.connectorLogin = &connectorLoginPanel{kind: managedGitHub, auth: common.WorkbenchConnectorAuthorization{ID: "auth", Status: "authorized", DisplayName: "alex"},
				choices: []protocol.ConfigOption{{Value: "123", Label: "example/pay"}, {Value: "456", Label: "example/ops"}}, selected: map[string]bool{"123": true, "456": true}}
			return ui.NewTester(m.view, 680, 580)
		}},
		{"uc07-task-list-en", 1190, 730, true, func(m *mockup) *ui.Tester { m.page = tasksPage; return ui.NewTester(m.view, 1190, 730) }},
		{"uc09-detail-zh", 860, 580, false, func(m *mockup) *ui.Tester {
			m.page = tasksPage
			m.openFullTask(0)
			m.taskDetailUI(m.objects[0].ID).connectorDetail = &connectorTaskDetailState{version: "v1", segment: "description", loaded: true, page: common.WorkbenchTaskDetail{Items: []protocol.DetailItem{
				{ID: "description", Text: &protocol.RichText{Format: "markdown", Content: protocol.Value(protocol.FieldText, "核对账单导出并记录结果。").Value}},
			}}}
			return ui.NewTester(m.view, 860, 580)
		}},
		{"uc11-mapping-en", 860, 580, true, func(m *mockup) *ui.Tester {
			m.page = connectionsPage
			sample := m.objects[0]
			sample.DueDate = "2026-12-01"
			m.connectorMapping = &connectorMappingPanel{scope: "pay", task: sample.ID, catalog: protocol.FieldCatalog{Fields: []protocol.FieldDefinition{
				{ID: "due_date", Label: "Due date", Type: protocol.FieldDate, Writable: true}, {ID: "promised", Label: "Promised date", Type: protocol.FieldDate, Writable: true},
			}}, draft: map[string]protocol.MappingRule{"due_date": {Field: "promised", Write: true}}, value: common.WorkbenchMappingResult{Sample: &sample}}
			return ui.NewTester(m.view, 860, 580)
		}},
		{"uc13-conflict-en", 680, 580, true, func(m *mockup) *ui.Tester {
			task := m.objects[0]
			m.connectorConflicts = map[string]common.WorkbenchTaskReceipt{task.ID: {SyncState: "conflict", Conflict: &common.WorkbenchFieldConflict{
				Current: map[string]protocol.FieldValue{"due_date": protocol.Value(protocol.FieldDate, "2026-12-15")}, Proposed: map[string]protocol.FieldValue{"due_date": protocol.Value(protocol.FieldDate, "2026-12-01")},
			}}}
			return ui.NewTester(func(c *ui.Context) { components.ApplyTheme(c); m.connectorConflictView(c, task) }, 680, 580)
		}},
		{"uc14-state-action-zh", 680, 580, false, func(m *mockup) *ui.Tester {
			m.page = tasksPage
			m.connectorTaskEditor = &connectorTaskEditor{task: m.objects[0], actionMode: true, action: "resolve",
				actions:      []protocol.StateAction{{ID: "resolve", Label: "Resolve", Required: []protocol.FieldDefinition{{ID: "resolution", Label: "Resolution", Type: protocol.FieldText}}}},
				actionValues: map[string]string{"resolution": "验证完成"}}
			return ui.NewTester(m.view, 680, 580)
		}},
		{"uc18-run-details-en", 860, 580, true, func(m *mockup) *ui.Tester {
			m.page = connectionsPage
			run := *m.connectorParents[0].Scopes[1].LastRun
			run.ErrorKind, run.FailedStage = "rate_limited", "read"
			retry := time.Now().Add(time.Hour)
			run.RetryAt = &retry
			m.connectorRuns = &connectorRunPanel{detail: &run}
			return ui.NewTester(m.view, 860, 580)
		}},
		{"uc19-usage-zh", 1190, 730, false, func(m *mockup) *ui.Tester {
			m.page, m.usageTab = usagePage, 2
			m.connectorUsage = connectorUsageState{loaded: true, items: []common.WorkbenchSyncUsage{
				{ConnectorID: managedGitHub, ConnectionID: "parent", ScopeID: "pay", Mode: "full", Runs: 1, Success: 1, Requests: 2, Fetched: 2, DurationMS: 373},
				{ConnectorID: managedGitHub, ConnectionID: "parent", ScopeID: "ops", Mode: "delta", Runs: 3, Success: 2, Failed: 1, NoChanges: 2, Requests: 5, RateLimits: 1, PartialRuns: 1, DurationMS: 723},
			}}
			return ui.NewTester(m.view, 1190, 730)
		}},
	}
	for _, spec := range specimens {
		englishUI.Store(spec.en)
		view := spec.view(connectorUIFixture())
		image := view.Image()
		if image == nil || image.Bounds().Dx() < spec.width || image.Bounds().Dy() < spec.height {
			t.Fatalf("%s: incomplete UI frame", spec.name)
		}
		f, err := os.Create(filepath.Join(dir, spec.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, image)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("%s: %v %v", spec.name, err, closeErr)
		}
		t.Log(spec.name)
	}
}

func TestConnector011UnknownOperationPreservesRequestAndConfirmedFields(t *testing.T) {
	m := connectorUIFixture()
	task := m.objects[0]
	m.liveTaskRequests = map[string]common.WorkbenchTaskWrite{task.ID: {RequestID: "original", SourceObjectID: task.ID,
		ConnectionID: task.ConnectionID, ConnectionVersion: task.ConnectionVersion, TargetVersion: task.TargetVersion,
		Fields: map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "First draft")},
	}}
	m.liveTaskProblems = map[string]string{}
	m.pendingTaskEdits[task.ID] = true
	m.finishLiveTaskToggle(task.ID, common.WorkbenchTaskReceipt{RequestID: "original", SourceObjectID: task.ID, SyncState: "needs_review"}, nil)
	if m.objects[0].Title != task.Title || m.serverTaskOperations[task.ID].RequestID != "original" {
		t.Fatal("uncertain operation changed confirmed state")
	}
	editor := &connectorTaskEditor{task: task, fields: []protocol.FieldDefinition{{ID: "title", Type: protocol.FieldText}}, values: map[string]string{"title": "Other draft"}, original: map[string]string{"title": task.Title}}
	m.submitConnectorEditor(editor)
	if editor.problem == "" || editor.saving || m.liveTaskRequests[task.ID].RequestID != "original" {
		t.Fatal("editor replaced an uncertain operation with a new request")
	}
}

func TestConnector011DesktopReviewActionConflictAndRepositoryLabels(t *testing.T) {
	old := englishUI.Swap(false)
	t.Cleanup(func() { englishUI.Store(old) })
	m := connectorUIFixture()
	task := m.objects[0]
	m.connectorConflicts = map[string]common.WorkbenchTaskReceipt{task.ID: {Conflict: &common.WorkbenchFieldConflict{
		Action: "close_not_planned", Current: map[string]protocol.FieldValue{"state": protocol.Value(protocol.FieldText, "closed")},
	}}}
	view := ui.NewTester(func(c *ui.Context) { m.connectorConflictView(c, task) }, 680, 580)
	if !view.HasText("状态操作 · 关闭为不计划处理") || !view.HasText("状态 · 来源值 closed") {
		t.Fatalf("action conflict: %v", view.Texts())
	}

	editor := &connectorTaskEditor{task: task, repositoryLabels: []string{"options", "label"}}
	options := []protocol.Option{{ID: "添加仓库标签", Label: "添加仓库标签"}, {ID: "available, too", Label: "available, too"}}
	view = ui.NewTester(func(c *ui.Context) { editor.editRepositoryLabels(c, options) }, 680, 580)
	for _, label := range []string{"available, too", "添加仓库标签"} {
		click(t, view, "添加仓库标签")
		if err := view.ChooseMenuItem(label); err != nil {
			t.Fatal(err)
		}
		if editor.repositoryLabels[len(editor.repositoryLabels)-1] != label {
			t.Fatalf("chosen labels: %v", editor.repositoryLabels)
		}
	}
	if len(editor.repositoryLabels) != 4 || !view.HasText("options") || !view.HasText("label") {
		t.Fatalf("chosen labels: %v, UI: %v", editor.repositoryLabels, view.Texts())
	}
}

func TestConnector011DesktopRetriesConflictInOriginalEditor(t *testing.T) {
	for _, actionMode := range []bool{false, true} {
		m := connectorUIFixture()
		task := m.objects[0]
		m.page, m.taskSelection = tasksPage, task.ID
		started := time.Now()
		m.trackConnectorConflictRefresh(task, 10, true, actionMode)
		if !actionMode {
			task.TargetVersion = "refreshed"
		}
		finished := started.Add(time.Second)
		snapshot := common.WorkbenchSnapshot{ParentConnectionID: "parent", Attempts: []common.WorkbenchAttempt{{ID: 10, Started: started, Finished: &finished, State: "success"}}, WorkbenchConnection: common.WorkbenchConnection{
			ID: task.ConnectionID, Kind: managedGitHub, Name: "example/pay", WriteEnabled: true, AccessState: "available", Version: 1,
		}}
		m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
		if editor := m.connectorTaskEditor; editor == nil || editor.actionMode != actionMode || editor.task.TargetVersion != task.TargetVersion || len(m.connectorConflictRefreshes) != 0 {
			t.Fatalf("retry action mode %v: %+v", actionMode, editor)
		}
	}
}

func TestConnector011DesktopConflictButtonsVisibleInDefaultSplit(t *testing.T) {
	for _, size := range [][2]int{{680, 580}, {1190, 730}} {
		for _, english := range []bool{false, true} {
			old := englishUI.Swap(english)
			m := connectorUIFixture()
			task := m.objects[0]
			m.page, m.taskSelection = tasksPage, task.ID
			previous := protocol.Value(protocol.FieldText, "open")
			m.connectorConflicts = map[string]common.WorkbenchTaskReceipt{task.ID: {
				Reason: "Source changed since the operation was prepared",
				Conflict: &common.WorkbenchFieldConflict{
					Action: "close", PreviousState: &previous,
					Current: map[string]protocol.FieldValue{"state": protocol.Value(protocol.FieldText, "closed"), "state_reason": protocol.Value(protocol.FieldText, "completed")},
				},
			}}
			view := ui.NewTester(m.view, size[0], size[1])
			for _, label := range []string{tr("接受来源值"), tr("重新编辑")} {
				box, visible := view.Find(label)
				if !visible || box.W <= 0 || box.H <= 0 || box.X < 0 || box.Y < 0 ||
					box.X+box.W > float32(size[0]) || box.Y+box.H > float32(size[1])-20 {
					t.Errorf("%v english=%v %s bounds %+v, visible=%v", size, english, label, box, visible)
				}
			}
			if dir := os.Getenv("APILOT_TEST_UI_EVIDENCE_DIR"); dir != "" && size[0] == 680 {
				language := "zh"
				if english {
					language = "en"
				}
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				file, err := os.Create(filepath.Join(dir, "conflict-default-split-"+language+".png"))
				if err != nil {
					t.Fatal(err)
				}
				if err, closeErr := png.Encode(file, view.Image()), file.Close(); err != nil || closeErr != nil {
					t.Fatalf("write conflict screenshot: %v, %v", err, closeErr)
				}
			}
			click(t, view, tr("重新编辑"))
			if !m.pendingSync[task.ConnectionID] {
				t.Fatal("re-edit did not request a scope refresh")
			}
			englishUI.Store(old)
		}
	}
}

func TestConnector011DesktopAcceptSourceKeepsRecoveryUntilSyncSucceeds(t *testing.T) {
	old := englishUI.Swap(false)
	t.Cleanup(func() { englishUI.Store(old) })
	for _, failFirst := range []bool{false, true} {
		m := connectorUIFixture()
		m.liveTaskProblems = map[string]string{}
		task := m.objects[0]
		m.page, m.taskSelection = tasksPage, task.ID
		m.connectorConflicts = map[string]common.WorkbenchTaskReceipt{task.ID: {
			Conflict: &common.WorkbenchFieldConflict{Action: "close", Current: map[string]protocol.FieldValue{
				"state": protocol.Value(protocol.FieldText, "closed"),
			}},
		}}
		snapshot := common.WorkbenchSnapshot{ParentConnectionID: "parent", WorkbenchConnection: common.WorkbenchConnection{
			ID: task.ConnectionID, Kind: managedGitHub, Name: "example/pay", WriteEnabled: true, AccessState: "available", Version: 1,
		}}
		view := ui.NewTester(m.view, 680, 580)
		click(t, view, "接受来源值")
		if !m.pendingSync[task.ConnectionID] {
			t.Fatal("accept source did not request a refresh")
		}
		started := time.Now()
		finished := started.Add(time.Second)
		attempt := common.WorkbenchAttempt{ID: 10, Started: started, State: "running"}
		m.trackConnectorConflictRefresh(task, attempt.ID, false, true)
		snapshot.Attempts = []common.WorkbenchAttempt{attempt}
		m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
		if m.connectorConflicts[task.ID].Conflict == nil {
			t.Fatal("accepted refresh prematurely cleared conflict")
		}
		if failFirst {
			attempt.State, attempt.Finished, attempt.Error = "failed", &finished, "temporary"
			snapshot.Attempts[0] = attempt
			m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
			if len(m.connectorConflictRefreshes) != 0 || m.connectorConflicts[task.ID].Conflict == nil ||
				m.liveTaskProblems[task.ID] != "同步失败" || m.objects[0].State != "open" {
				t.Fatal("failed refresh lost recovery or changed confirmed state")
			}
			view = ui.NewTester(m.view, 680, 580)
			if !view.HasText("同步失败") || !view.HasText("重新编辑") {
				t.Fatalf("failed refresh omitted recovery feedback: %v", view.Texts())
			}
			click(t, view, "接受来源值")
			if !m.pendingSync[task.ConnectionID] {
				t.Fatal("the same button did not retry the refresh")
			}
			attempt.ID++
			m.trackConnectorConflictRefresh(task, attempt.ID, false, true)
		}
		attempt.State, attempt.Finished, attempt.Error = "success", &finished, ""
		snapshot.Attempts[0] = attempt
		task.State, task.TargetVersion = "done", "closed-version"
		m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
		if len(m.connectorConflictRefreshes) != 0 || m.connectorConflicts[task.ID].Conflict != nil ||
			m.liveTaskProblems[task.ID] != "" || m.connectorTaskEditor != nil || m.objects[0].State != "done" {
			t.Fatal("successful refresh did not settle Keep source value")
		}
	}
}

func TestConnector011DesktopConcurrentConflictRecovery(t *testing.T) {
	old := englishUI.Swap(false)
	t.Cleanup(func() { englishUI.Store(old) })
	for _, successFirst := range []bool{true, false} {
		name := "failure-first"
		if successFirst {
			name = "success-first"
		}
		t.Run(name, func(t *testing.T) {
			m := connectorUIFixture()
			pay, ops := m.objects[0], m.objects[1]
			m.liveTaskProblems = map[string]string{}
			conflict := common.WorkbenchTaskReceipt{Conflict: &common.WorkbenchFieldConflict{Action: "close"}}
			m.connectorConflicts = map[string]common.WorkbenchTaskReceipt{pay.ID: conflict, ops.ID: conflict}
			m.trackConnectorConflictRefresh(pay, 10, false, true)
			m.trackConnectorConflictRefresh(ops, 11, false, true)

			finished := time.Now()
			snapshot := func(task sourceObject, id int64) common.WorkbenchSnapshot {
				return common.WorkbenchSnapshot{ParentConnectionID: "parent", WorkbenchConnection: common.WorkbenchConnection{
					ID: task.ConnectionID, Kind: managedGitHub, Name: task.ConnectionID, WriteEnabled: true, AccessState: "available", Version: 1,
				}, Attempts: []common.WorkbenchAttempt{{ID: id, State: "running"}}}
			}
			paySnapshot, opsSnapshot := snapshot(pay, 10), snapshot(ops, 11)
			apply := func() {
				m.applyLive([]common.WorkbenchSnapshot{paySnapshot, opsSnapshot}, []sourceObject{pay, ops})
			}
			view := func(task sourceObject) *ui.Tester {
				return ui.NewTester(func(c *ui.Context) { m.connectorConflictView(c, task) }, 680, 580)
			}
			apply()
			for _, task := range []sourceObject{pay, ops} {
				if !view(task).HasText("正在同步") {
					t.Errorf("%s: running refresh has no status", task.ConnectionID)
				}
			}

			success := func() {
				paySnapshot.Attempts[0].State, paySnapshot.Attempts[0].Finished = "success", &finished
				pay.State = "done"
				apply()
				if m.connectorConflicts[pay.ID].Conflict != nil || m.liveTaskProblems[pay.ID] != "" {
					t.Error("successful PAY refresh did not close its conflict")
				}
			}
			failure := func() {
				opsSnapshot.Attempts[0].State, opsSnapshot.Attempts[0].Finished = "failed", &finished
				apply()
				if m.connectorConflicts[ops.ID].Conflict == nil || m.liveTaskProblems[ops.ID] != "同步失败" || view(ops).HasText("正在同步") {
					t.Error("failed OPS refresh did not return to recovery")
				}
			}
			if successFirst {
				success()
				if !view(ops).HasText("正在同步") {
					t.Error("PAY success lost ongoing OPS recovery")
				}
				failure()
			} else {
				failure()
				if !view(pay).HasText("正在同步") {
					t.Error("OPS failure lost ongoing PAY recovery")
				}
				success()
			}
		})
	}
}

func TestConnector011DesktopDetailReloadsAfterScopeMappingChange(t *testing.T) {
	m := connectorUIFixture()
	pay, ops := m.objects[0], m.objects[1]
	pay.Kind, ops.Kind = managedFixture, managedFixture
	payDetail, opsDetail := m.taskDetailUI(pay.ID), m.taskDetailUI(ops.ID)
	payDetail.draft = "Local draft"
	for _, entry := range []struct {
		task   sourceObject
		detail *taskDetailUI
	}{{pay, payDetail}, {ops, opsDetail}} {
		entry.detail.connectorDetail = &connectorTaskDetailState{
			version: entry.task.TargetVersion, segment: "description", loaded: true,
			page: common.WorkbenchTaskDetail{Items: []protocol.DetailItem{{
				ID: "description", Text: &protocol.RichText{Content: protocol.Value(protocol.FieldRichText, "Original description").Value},
			}}},
		}
	}
	render := func(task *sourceObject, detail *taskDetailUI) *ui.Tester {
		return ui.NewTester(func(c *ui.Context) { m.managedTaskDetail(c, task, detail) }, 680, 580)
	}
	if view := render(&pay, payDetail); !view.HasText("Original description") {
		t.Fatal("PAY detail did not load before the mapping save")
	}
	payCache, opsCache := payDetail.connectorDetail, opsDetail.connectorDetail
	// A live refresh reads the scope revision after the CLI saves a description-only rule.
	m.connectorParents[0].Scopes[0].MappingRevision++
	if view := render(&pay, payDetail); view.HasText("Original description") || payDetail.connectorDetail == payCache {
		t.Fatal("PAY retained its cached description after the mapping revision changed")
	}
	if payDetail.connectorDetail.segment != "description" || payDetail.draft != "Local draft" {
		t.Fatal("mapping refresh lost the selected segment or local draft")
	}
	if view := render(&ops, opsDetail); !view.HasText("Original description") || opsDetail.connectorDetail != opsCache {
		t.Fatal("PAY mapping change invalidated the independent OPS detail")
	}
}
