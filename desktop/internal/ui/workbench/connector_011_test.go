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

	m.connectorEditor011 = &connectorTaskEditor{task: task, actionMode: true, action: "resolve",
		actions:      []protocol.StateAction{{ID: "resolve", Label: "Resolve", Required: []protocol.FieldDefinition{{ID: "resolution", Label: "Resolution", Type: protocol.FieldText}}}},
		actionValues: map[string]string{}}
	view = ui.NewTester(m.connectorTaskEditorDialog, 680, 580)
	click(t, view, "提交")
	if !view.HasText("填写操作所需字段 · 解决结果") || m.sourceWritePending(task.ID) {
		t.Fatal("missing transition input was dispatched")
	}
	view.Key(0, ui.KeyEscape)
	if m.connectorEditor011 != nil {
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
			m.connectorEditor011 = &connectorTaskEditor{task: m.objects[0], actionMode: true, action: "resolve",
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
		m.retryConnectorEdit = &connectorConflictRetry{taskID: task.ID, scopeID: task.ConnectionID, version: task.TargetVersion, actionMode: actionMode}
		task.TargetVersion = "refreshed"
		snapshot := common.WorkbenchSnapshot{ParentConnectionID: "parent", WorkbenchConnection: common.WorkbenchConnection{
			ID: task.ConnectionID, Kind: managedGitHub, Name: "example/pay", WriteEnabled: true, AccessState: "available", Version: 1,
		}}
		m.applyLive([]common.WorkbenchSnapshot{snapshot}, []sourceObject{task})
		if editor := m.connectorEditor011; editor == nil || editor.actionMode != actionMode || editor.task.TargetVersion != "refreshed" || m.retryConnectorEdit != nil {
			t.Fatalf("retry action mode %v: %+v", actionMode, editor)
		}
	}
}
