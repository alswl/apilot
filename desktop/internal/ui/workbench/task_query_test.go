package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"cel.dev/cel-go/cel"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/egoist/mygo/ui"
)

func TestTaskQueryViewMigrationAndConflicts(t *testing.T) {
	dir := t.TempDir()
	old, path := filepath.Join(dir, "task-views.json"), filepath.Join(dir, "task-views.v2.json")
	state := defaultTaskViewState("pending")
	state.Query, state.Source, state.SourceID, state.Board, state.Page = `label:urgent | title:"two words"`, "Old scope", "stable-scope", true, 3
	state.PageSizeOverride = 20
	views := []savedTaskView{{ID: "first", Name: "Duplicate", State: state}, {ID: "second", Name: "Duplicate", State: state}}
	raw, err := json.Marshal(views)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := bootTaskQueryConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Revision != 1 || len(cfg.Views) != 2 || cfg.Views[0].Query.Engine != "legacy-v1" || !reflect.DeepEqual(cfg.Views[0].State, state) {
		t.Fatalf("migration lost data: %+v", cfg)
	}
	reloaded, err := bootTaskQueryConfig(path)
	if err != nil || !reflect.DeepEqual(cfg, reloaded) {
		t.Fatalf("migration is not idempotent: %v", err)
	}
	preserved, _ := os.ReadFile(old)
	if string(preserved) != string(raw) {
		t.Fatal("legacy bytes changed")
	}

	first, second := newFramework(), newFramework()
	for _, m := range []*mockup{first, second} {
		m.customQuery.path = path
		m.adoptTaskQueryConfig(cfg)
	}
	first.selectTaskView("first")
	first.search = "draft"
	first.queryTabAction("rename")
	first.customQuery.name = "Renamed"
	if !first.applyQueryTabAction() || first.search != "draft" || first.savedTaskViews[0].State.Query != state.Query {
		t.Fatal("rename committed a draft")
	}
	if err := second.updateTaskQueryConfig(func(*taskQueryConfig) error { t.Fatal("conflicting callback ran"); return nil }); !errors.Is(err, errTaskQueryConfigChanged) {
		t.Fatalf("stale revision: %v", err)
	}
	if second.savedTaskViews[0].Name != "Renamed" {
		t.Fatal("latest configuration was not reloaded")
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bootTaskQueryConfig(path); err == nil {
		t.Fatal("unknown schema accepted")
	}
	if data, _ := os.ReadFile(path); string(data) != `{"schemaVersion":99}` {
		t.Fatal("unknown schema overwritten")
	}
}

func TestTaskQueryExactGitHubLookupControlAndSavedView(t *testing.T) {
	previousLanguage := englishUI.Swap(true)
	t.Cleanup(func() { englishUI.Store(previousLanguage) })
	path := filepath.Join(t.TempDir(), "task-views.v2.json")
	view := taskQueryView{Engine: "cel", Environment: 1, Keyword: "17", Expression: `task.status == "open"`}
	cfg := taskQueryConfig{SchemaVersion: 2, Revision: 1, Timezone: "UTC", Fields: []common.WorkbenchQueryField{}, Views: []savedTaskView{{ID: "saved", Name: "Saved", Query: &view}}}
	if _, err := writeTaskQueryConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	m := newFramework()
	m.customQuery.path = path
	m.adoptTaskQueryConfig(cfg)
	m.customQuery.config.Timezone = "UTC"
	m.customQuery.result = &common.WorkbenchTaskQueryResult{}
	m.customQuery.valid = true
	m.taskView = "saved"
	m.sourceFilter, m.sourceFilterID, m.statusFilter = "Repo A", "repo-a", "待处理"
	m.connectionsData = []connection{{ID: "repo-a", Name: "Repo A"}}
	preview := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { m.customQueryControls(c, *m.activeTaskQuery()) })
	}, 860, 580)
	if !preview.HasText("Find Issue") || !preview.HasText("Search tasks") {
		t.Fatalf("exact lookup or ordinary search is not discoverable: %v", preview.Texts())
	}
	click(t, preview, "Find Issue")
	if !preview.HasText("Find a GitHub Issue exactly") || !preview.HasText("Repository") || !preview.HasText("Issue number") {
		t.Fatalf("repository lookup dialog is incomplete: %v", preview.Texts())
	}
	preview.Type("owner/repo")
	click(t, preview, "Issue number")
	preview.Type("17")
	click(t, preview, "Apply")
	draft := m.activeTaskQuery()
	if draft == nil || draft.IssueRepository != "owner/repo" || draft.IssueNumber != "17" || draft.StableSourceID != "" {
		t.Fatalf("lookup controls did not create a repo-scoped selector: %+v", draft)
	}
	input := m.queryInput(*draft)
	if input.IssueRepository != "owner/repo" || input.IssueNumber != "17" || input.Keyword != "17" || input.Expression != `task.status == "open"` ||
		!slices.Equal(input.SourceIDs, []string{"repo-a"}) || !slices.Equal(input.States, []string{"open"}) {
		t.Fatalf("exact lookup did not combine with existing filters: %+v", input)
	}
	m.customQuery.valid = true
	m.saveCustomQuery()
	saved, err := readTaskQueryConfig(path)
	if err != nil || saved.Views[0].Query.IssueRepository != "owner/repo" || saved.Views[0].Query.IssueNumber != "17" || saved.Views[0].Query.Keyword != "17" {
		t.Fatalf("saved view did not retain lookup and keyword: %+v %v", saved.Views, err)
	}
	click(t, preview, "owner/repo #17 ×")
	clearedDraft := m.activeTaskQuery()
	if clearedDraft == nil || taskQueryLookupSummary(*clearedDraft) != "" || clearedDraft.Keyword != "17" || clearedDraft.Expression != `task.status == "open"` {
		t.Fatalf("removing lookup changed keyword or CEL filters: %+v", clearedDraft)
	}
	m.customQuery.valid = true
	m.saveCustomQuery()
	saved, err = readTaskQueryConfig(path)
	if err != nil || saved.Views[0].Query.IssueRepository != "" || saved.Views[0].Query.IssueNumber != "" || saved.Views[0].Query.Keyword != "17" {
		t.Fatalf("saved view did not persist clearing only the lookup: %+v %v", saved.Views, err)
	}
	cleared := *saved.Views[0].Query
	cleared.clearGitHubLookup()
	if cleared.Keyword != "17" || cleared.Expression != `task.status == "open"` || taskQueryLookupSummary(cleared) != "" {
		t.Fatalf("clearing exact lookup changed ordinary query state: %+v", cleared)
	}
	if err := cleared.setGitHubLookup(taskLookupStableID, "117", "owner/repo", "17"); err != nil ||
		cleared.StableSourceID != "117" || cleared.IssueRepository != "" || cleared.IssueNumber != "" {
		t.Fatalf("stable ID mode did not replace the repository selector: %+v %v", cleared, err)
	}
	stableInput := m.queryInput(cleared)
	if stableInput.StableSourceID != "117" || stableInput.IssueRepository != "" || stableInput.IssueNumber != "" || stableInput.Keyword != "17" {
		t.Fatalf("stable ID lookup did not map with existing query filters: %+v", stableInput)
	}
}

func TestTaskQueryLookupModalOwnsKeyboardAndDismissal(t *testing.T) {
	previousLanguage := englishUI.Swap(true)
	t.Cleanup(func() { englishUI.Store(previousLanguage) })
	view := taskQueryView{Engine: "cel", Environment: 1}
	m := newFramework()
	m.page, m.taskView = tasksPage, "saved"
	m.customQuery.config.Timezone = "UTC"
	m.customQuery.drafts = map[string]taskQueryView{"saved": view}
	preview := ui.NewTester(func(c *ui.Context) {
		m.customQueryControls(c, *m.activeTaskQuery())
		m.dispatchKeyboard(c, m.activeCommands())
	}, 860, 580)
	click(t, preview, "Find Issue")
	if !m.customQuery.lookupOpen {
		t.Fatal("Find Issue modal did not open")
	}
	for i := 0; i < 8 && !preview.Focused("Cancel"); i++ {
		preview.Key(0, ui.KeyTab)
	}
	if !preview.Focused("Cancel") {
		t.Fatal("modal keyboard focus did not reach its Cancel button")
	}
	preview.Key(0, ui.KeyF)
	if m.customQuery.conditionOpen {
		t.Fatal("F opened the task condition popover from inside the lookup modal")
	}
	if err := preview.Click("Repository + issue number"); err != nil {
		t.Fatal(err)
	}
	preview.Key(ui.Cmd, ui.Key1)
	if m.page != tasksPage || !m.customQuery.lookupOpen {
		t.Fatalf("global navigation escaped the open lookup modal: page=%d open=%t", m.page, m.customQuery.lookupOpen)
	}
	preview.Key(0, ui.KeyF)
	if m.customQuery.conditionOpen {
		t.Fatal("F opened the task condition popover behind the lookup modal")
	}
	preview.Key(0, ui.KeyEscape)
	if !m.customQuery.lookupOpen || !preview.Focused("Repository + issue number") {
		t.Fatalf("Escape did not close the inner selector and restore its focus: open=%t focused=%t", m.customQuery.lookupOpen, preview.Focused("Repository + issue number"))
	}
	preview.Key(0, ui.KeyEscape)
	if m.customQuery.lookupOpen || !preview.Focused("Find Issue") {
		t.Fatalf("Escape did not close lookup and restore its trigger focus: open=%t focused=%t", m.customQuery.lookupOpen, preview.Focused("Find Issue"))
	}
}

func TestTaskQueryLookupBlocksGlobalKeyboardRoutes(t *testing.T) {
	m := newFramework()
	m.page = tasksPage
	m.customQuery.lookupOpen = true
	preview := ui.NewTester(func(c *ui.Context) { m.dispatchKeyboard(c, m.activeCommands()) }, 860, 580)
	preview.Key(ui.Cmd, ui.Key1)
	if m.page != tasksPage {
		t.Fatalf("global navigation ran behind the lookup modal: page=%d", m.page)
	}
}

func TestTaskQueryLookupClosesWhenContextChanges(t *testing.T) {
	m := newFramework()
	m.page = tasksPage
	m.customQuery.lookupOpen = true
	m.navigate(nowPage)
	if m.customQuery.lookupOpen {
		t.Fatal("page navigation left lookup modal open")
	}
	m.page = tasksPage
	m.taskView = "before"
	m.taskViewDrafts = make(map[string]taskViewState)
	m.customQuery.lookupOpen = true
	m.selectTaskView("after")
	if m.customQuery.lookupOpen {
		t.Fatal("saved-view selection left lookup modal open")
	}
}

func TestTaskQueryLookupUsesCoreRepositoryValidation(t *testing.T) {
	for _, repository := range []string{".", "..", "owner/.", "owner/..", "owner/re\tpo", "owner/re\u00a0po", "owner/repo?x", "owner/repo%2Fother", "owner/repo\\other"} {
		view := taskQueryView{}
		if err := view.setGitHubLookup(taskLookupIssue, "", repository, "17"); err == nil {
			t.Errorf("accepted Core-invalid repository %q", repository)
		}
	}
	view := taskQueryView{}
	if err := view.setGitHubLookup(taskLookupIssue, "", "owner/repo", "17"); err != nil || view.IssueRepository != "owner/repo" || view.IssueNumber != "17" {
		t.Fatalf("valid owner/repository lookup changed: %+v %v", view, err)
	}
}

func TestTaskQueryConditionsUseStandardCEL(t *testing.T) {
	env, err := cel.NewEnv(cel.Variable("task", cel.MapType(cel.StringType, cel.DynType)), cel.Variable("custom", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("week_start", cel.StringType), cel.Variable("next_week_start", cel.StringType))
	if err != nil {
		t.Fatal(err)
	}
	quoted := "double \" and \\ and\n 中文 🐈"
	var expression string
	for _, test := range []struct{ id, key, kind, op, value string }{
		{"task.labels", "labels", "multi_select", "包含", "project/lbkit"},
		{"task.status", "status", "single_select", "任选", "open,in_progress"},
		{"task.due", "due", "date", "本周", ""},
		{"estimate", "estimate", "number", "大于等于", "3"},
		{"detail", "detail", "text", "等于", quoted},
	} {
		part, err := taskQueryCondition(common.WorkbenchQueryFieldInfo{ID: test.id, Key: test.key, Type: test.kind}, test.op, test.value)
		if err != nil {
			t.Fatal(err)
		}
		expression = appendTaskQueryCondition(expression, part)
	}
	checked, issues := env.Compile(expression)
	if issues.Err() != nil {
		t.Fatalf("%s: %s", expression, issues)
	}
	p, err := env.Program(checked)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"task": map[string]any{"labels": []string{"project/lbkit"}, "status": "open", "due": "2026-11-23"},
		"custom": map[string]any{"estimate": 3.0, "detail": quoted}, "week_start": "2026-11-23", "next_week_start": "2026-11-30"}
	result, _, err := p.Eval(args)
	if err != nil || result.Value() != true {
		t.Fatalf("%s: %v %v", expression, result, err)
	}
	grouped := appendTaskQueryCondition("true || false", "false")
	if !strings.HasPrefix(grouped, "(true || false)") {
		t.Fatalf("handwritten OR lost grouping: %s", grouped)
	}
}

type taskQueryRunnerFunc func(context.Context, common.WorkbenchTaskQuery) (common.WorkbenchTaskQueryResult, error)

func (f taskQueryRunnerFunc) QueryWorkbenchTasks(ctx context.Context, input common.WorkbenchTaskQuery) (common.WorkbenchTaskQueryResult, error) {
	return f(ctx, input)
}

func TestTaskQueryPaginationRejectsMixedVisibility(t *testing.T) {
	calls := 0
	run := taskQueryRunnerFunc(func(_ context.Context, in common.WorkbenchTaskQuery) (common.WorkbenchTaskQueryResult, error) {
		calls++
		if calls == 1 {
			return common.WorkbenchTaskQueryResult{Revision: 2, AsOfDate: "2026-11-27", PageToken: "before", SliceTotal: 2, Items: []common.WorkbenchQueryItem{{Task: common.WorkbenchTask{ID: "revoked"}}}}, nil
		}
		if calls == 2 {
			if in.Offset != 1 || in.RequiredRevision != 2 || in.PageToken != "before" || in.AsOfDate != "2026-11-27" {
				t.Fatalf("continuation contract: %+v", in)
			}
			return common.WorkbenchTaskQueryResult{}, common.Err("STALE_REVISION", "visibility changed")
		}
		if in.Offset != 0 {
			t.Fatalf("retry must start from the first page: %+v", in)
		}
		return common.WorkbenchTaskQueryResult{Revision: 3, SliceTotal: 1, Items: []common.WorkbenchQueryItem{{Task: common.WorkbenchTask{ID: "permitted"}}}}, nil
	})
	result, page, err := loadCustomQuery(t.Context(), run, common.WorkbenchTaskQuery{Limit: 1, Display: common.WorkbenchQueryDisplay{Layout: "board"}}, 1)
	if err != nil || page != 0 || len(result.Items) != 1 || result.Items[0].Task.ID != "permitted" {
		t.Fatalf("result from old boundary: %+v / %v", result, err)
	}
	m := newFramework()
	m.customQuery.result = &result
	m.customQuery.fieldPreview = &result
	m.receiveLiveDisconnect()
	if m.customQuery.result != nil || m.customQuery.fieldPreview != nil {
		t.Fatal("disconnected snapshot still visible")
	}
}

func TestTaskQueryAutomaticPageKeepsAnchor(t *testing.T) {
	m := newFramework()
	m.customQuery.autoLimit, m.customQuery.page = 20, 2
	view := taskQueryView{}
	if !m.fitCustomQueryPage(314, false) || m.queryInput(view).Limit != 10 || m.customQuery.page != 4 {
		t.Fatalf("resized page lost anchor: limit=%d page=%d", m.queryInput(view).Limit, m.customQuery.page)
	}
	m.inspector = true
	if m.fitCustomQueryPage(180, false) || m.customQuery.page != 4 {
		t.Fatal("opening detail moved the page")
	}
	m.inspector = false
	m.taskPageSizeOverride = 50
	if m.fitCustomQueryPage(180, false) || m.queryInput(view).Limit != 50 {
		t.Fatal("automatic sizing replaced an explicit page size")
	}
}

func TestTaskQueryConversionShowsDifferenceIDs(t *testing.T) {
	items := []common.WorkbenchQueryItem{
		{Task: common.WorkbenchTask{ID: "added-z"}},
		{Task: common.WorkbenchTask{ID: "common"}},
		{Task: common.WorkbenchTask{ID: "added-a"}},
	}
	comparison := taskQueryComparison([]string{"removed-z", "common", "removed-a"}, items)
	for _, want := range []string{"原查询 3 项，CEL 3 项，新增 2 项，移除 2 项", "新增任务 ID\nadded-a\nadded-z", "移除任务 ID\nremoved-a\nremoved-z"} {
		if !strings.Contains(comparison, want) {
			t.Fatalf("missing %q: %q", want, comparison)
		}
	}
	if strings.Contains(comparison, "common") {
		t.Fatalf("shared tasks are not differences: %q", comparison)
	}
	m := newFramework()
	m.taskView = "converted"
	m.customQuery.conversion = map[string]taskQueryConversion{m.taskView: {}}
	m.customQuery.comparison = comparison
	p := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() { m.customQueryControls(c, taskQueryView{}) })
	}, 680, 580)
	if !p.HasText("added-a") || !p.HasText("removed-z") {
		t.Fatal("comparison panel does not expose the differing IDs")
	}
}

func TestTaskQueryFieldDeleteRequiresConfirmation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-views.v2.json")
	field := common.WorkbenchQueryField{ID: "points", Name: "Points", Key: "points", Type: "number"}
	cfg := taskQueryConfig{SchemaVersion: 2, Revision: 1, Timezone: "UTC", Fields: []common.WorkbenchQueryField{field},
		Views: []savedTaskView{
			{ID: "query", Name: "Query reference", Query: &taskQueryView{Engine: "cel", Environment: 1, Dependencies: []string{field.ID}}},
			{ID: "board", Name: "Board reference", Query: &taskQueryView{Engine: "cel", Environment: 1, Board: common.WorkbenchQueryDisplay{SumBy: field.ID}}},
		},
	}
	if _, err := writeTaskQueryConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	m := newFramework()
	m.customQuery.path = path
	m.adoptTaskQueryConfig(cfg)
	m.customQuery.field, m.customQuery.fieldID, m.customQuery.fieldsOpen = &field, field.ID, true
	p := ui.NewTester(m.queryFieldsDialog, 880, 680)
	click(t, p, "删除")
	for _, label := range []string{"删除字段「Points」？", "Query reference", "Board reference"} {
		if !p.HasText(label) {
			t.Fatalf("missing confirmation content: %q", label)
		}
	}
	assertStored := func(revision int64, fields int) {
		t.Helper()
		actual, err := readTaskQueryConfig(path)
		if err != nil || actual.Revision != revision || len(actual.Fields) != fields {
			t.Fatalf("stored configuration: %+v, %v", actual, err)
		}
	}
	assertStored(1, 1)
	click(t, p, "取消")

	// Use the same transaction as editing the affected saved views.
	if err := m.updateTaskQueryConfig(func(cfg *taskQueryConfig) error {
		cfg.Views[0].Query.Dependencies = nil
		cfg.Views[1].Query.Board.SumBy = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p.Frame()
	click(t, p, "删除")
	assertStored(2, 1)
	if !p.HasText("没有已保存的视图引用此字段") {
		t.Fatal("missing no-references confirmation")
	}
	click(t, p, "确认删除")
	assertStored(3, 0)
}
