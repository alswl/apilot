package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
