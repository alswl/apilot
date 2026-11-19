package workbench

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTaskQueryCombinesLabelsAndSourceProperties(t *testing.T) {
	m := newMockup()
	for _, tc := range []struct {
		query string
		want  []int
	}{
		{"label:=customer", []int{1}},
		{"#refund -label:customer", []int{2, 0}},
		{`source:obsidian path:Payments section:"Acceptance"`, []int{0}},
		{"source:github repo:=example/payments assignee:lin milestone:refund", []int{2}},
		{"source:obsidian repo:payments", nil},
		{"section:follow-up | id:#40", []int{1, 2}},
		{"has:repo", []int{2}},
		{"-has:repo", []int{1, 0}},
	} {
		m.search = tc.query
		if got := m.taskRows(); !slices.Equal(got, tc.want) {
			t.Errorf("query %q matched %v, want %v", tc.query, got, tc.want)
		}
	}
}

func TestTaskQueryReportsMalformedConditions(t *testing.T) {
	m := newMockup()
	for _, query := range []string{"unknown:x", `title:"refund`, "label:", "label:=refund |", "has:missing"} {
		m.search = query
		if _, message := m.parseSearch(); message == "" {
			t.Errorf("expected diagnostic for %q", query)
		}
	}
}

func TestTaskSearchFieldsAndLabelPicker(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	m.sourceFilter = "支付发布"
	p := ui.NewTester(m.view, 1190, 730)
	click(t, p, "搜索字段")
	if texts := p.Texts(); !slices.Contains(texts, "repo:") || slices.Contains(texts, "path:") ||
		slices.Contains(texts, "新建任务") {
		t.Fatal("field guide must list the selected source's properties and skip the old New Task button")
	}
	click(t, p, "#documentation")
	if m.search != `label:="documentation"` || !p.Focused("搜索任务") ||
		!slices.Equal(m.taskRows(), []int{2}) {
		t.Fatal("clicking a label must apply the exact filter and return focus to search")
	}
}

func TestObsidianSourceLinkAndTaskFields(t *testing.T) {
	if got := obsidianNoteURL("/Vaults/我的 Vault", "Work/Plan + 1.md"); got != "obsidian://open?path=%2FVaults%2F%E6%88%91%E7%9A%84%20Vault%2FWork%2FPlan%20%2B%201.md" {
		t.Fatalf("unexpected Obsidian URI: %q", got)
	}
	if obsidianNoteURL("/Vaults/Test", "../Other.md") != "" {
		t.Fatal("accepted a note outside the configured vault")
	}
	m := newMockup()
	m.connectionsData = []connection{{ID: "vault", Name: "Test vault", SearchFields: []string{"file", "recurrence", "depends_on"}}}
	task := sourceObject{ConnectionID: "vault", NativeFields: map[string]string{
		"path": "Work/Plan.md", "recurrence": "every week", "depends_on": "one,two",
	}}
	m.search = `file:=Plan.md recurrence:"every week" depends_on:=two`
	query, problem := m.parseSearch()
	if problem != "" || !m.matchesTask(task, query) {
		t.Fatalf("could not filter by Obsidian task attributes: %s", problem)
	}
}

func TestSourceLogoFiltersToggleConnection(t *testing.T) {
	m := newMockup()
	m.connectionsData = []connection{
		{ID: "one", Name: "First vault", SourceKind: obsidianTasks},
		{ID: "two", Name: "Second vault", SourceKind: obsidianTasks},
	}
	p := ui.NewTester(m.sourceLogoFilters, 380, 100)
	click(t, p, "First vault")
	if m.sourceFilter != "First vault" {
		t.Fatalf("source filter after click: %q", m.sourceFilter)
	}
	click(t, p, "First vault")
	if m.sourceFilter != "全部来源" {
		t.Fatalf("source filter after repeated click: %q", m.sourceFilter)
	}
}
