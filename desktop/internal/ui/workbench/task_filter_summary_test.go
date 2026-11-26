package workbench

import (
	"strings"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/egoist/mygo/ui"
)

func TestActiveTaskFiltersRemainVisibleAfterDismissal(t *testing.T) {
	previous := englishUI.Swap(true)
	t.Cleanup(func() { englishUI.Store(previous) })
	m := newFramework()
	m.live = services.NewRemote("")
	m.useAdaptiveLayout()
	m.navigate(tasksPage)
	alpha := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "alpha", Name: "QA Local Alpha", Kind: common.WorkbenchLocal}}
	beta := common.WorkbenchSnapshot{WorkbenchConnection: common.WorkbenchConnection{ID: "beta", Name: "QA Local Beta", Kind: common.WorkbenchLocal}}
	tasks := []sourceObject{
		{ID: "alpha-open", ConnectionID: "alpha", Title: "Shared", State: "open"},
		{ID: "alpha-done", ConnectionID: "alpha", Title: "Shared completed", State: "done"},
		{ID: "beta-open", ConnectionID: "beta", Title: "Shared", State: "open"},
		{ID: "alpha-other", ConnectionID: "alpha", Title: "Charlie", State: "open"},
	}
	m.applyLive([]common.WorkbenchSnapshot{alpha, beta}, tasks)
	p := ui.NewTester(m.view, 880, 680)
	assertSummary := func(label string, want bool) {
		t.Helper()
		box, rendered := p.Find(label)
		_, accessible := p.AccessByRole(ui.RoleText, label)
		if rendered != want || accessible != want || (want && (box.W <= 0 || box.H <= 0)) {
			t.Fatalf("%q rendered=%v accessible=%v bounds=%+v, want present=%v", label, rendered, accessible, box, want)
		}
	}
	const source = "Source: QA Local Alpha"
	const status = "Status: To do"
	assertSummary(source, false)
	assertSummary(status, false)
	click(t, p, "QA Local Alpha", "Search tasks")
	p.Type("Shared")
	click(t, p, "Search fields", "All statuses", "To do")
	p.Key(0, ui.KeyEscape)
	if m.searchExpanded || !m.compactTaskList() || m.search != "Shared" || m.statusFilter != "待处理" {
		t.Fatalf("expected closed compact filters with the original query and status, got %+v", m.viewState())
	}
	assertSummary(source, true)
	assertSummary(status, true)
	if rows := m.taskRows(); len(rows) != 1 || m.objects[rows[0]].ID != "alpha-open" {
		t.Fatalf("expected only Alpha's open Shared task, got %v", rows)
	}

	p.SetSize(1190, 730)
	if m.compactTaskList() {
		t.Fatal("expected the inline status selector")
	}
	assertSummary(source, true)
	assertSummary(status, false)

	p.SetSize(680, 580)
	alpha.Name = strings.Repeat("Long source name ", 30)
	m.applyLive([]common.WorkbenchSnapshot{alpha, beta}, tasks)
	p.Frame()
	assertSummary(status, true)
	assertSummary("Source: "+alpha.Name, true)
	name, _ := p.Find("Source: " + alpha.Name)
	row, rowVisible := p.Find("Shared")
	footer, footerVisible := p.Find("Tasks per page")
	if name.X+name.W > 680 || !rowVisible || !footerVisible || name.Y+name.H > row.Y || row.Y+row.H > footer.Y || footer.Y+footer.H > 580 {
		t.Fatalf("expected the long source, result and pagination in order within the minimum window, got source=%+v row=%+v footer=%+v", name, row, footer)
	}
	englishUI.Store(false)
	p.Frame()
	assertSummary("状态：待处理", true)
	assertSummary("来源："+alpha.Name, true)
	englishUI.Store(true)

	m.applyLive([]common.WorkbenchSnapshot{beta}, tasks[2:3])
	p.Frame()
	assertSummary("Selected source unavailable", true)
	if len(m.taskRows()) != 0 {
		t.Fatal("a removed source must not select another source")
	}
	click(t, p, "Clear filters")
	assertSummary("Selected source unavailable", false)
	assertSummary(status, false)
	if len(m.taskRows()) != 1 {
		t.Fatal("clearing must restore the remaining source's task")
	}
}
