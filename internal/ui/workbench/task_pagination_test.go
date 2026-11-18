package workbench

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestTaskPaginationControlsUseFilteredResult(t *testing.T) {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	m.navigate(tasksPage)
	p := ui.NewTester(m.view, 1190, 730)
	assertPage := func(label string, size, count int) {
		t.Helper()
		if got := len(m.pagedTaskRows(m.taskRows())); got != count || m.taskPageSize != size || !p.HasText(label) {
			t.Fatalf("want %q and %d rows at size %d; got page %d with %d rows at size %d", label, count, size, m.taskPageIndex+1, got, m.taskPageSize)
		}
	}
	assertPage("第 1–10 项 · 共 21 项", 10, 10)
	click(t, p, "下一页")
	assertPage("第 11–20 项 · 共 21 项", 10, 10)
	click(t, p, "下一页")
	assertPage("第 21–21 项 · 共 21 项", 10, 1)
	click(t, p, "上一页")
	assertPage("第 11–20 项 · 共 21 项", 10, 10)

	click(t, p, "每页数量", "20")
	assertPage("第 1–20 项 · 共 21 项", 20, 20)
	click(t, p, "下一页")
	assertPage("第 21–21 项 · 共 21 项", 20, 1)
	p.Key(0, ui.KeySlash)
	p.Type("id:#40")
	assertPage("第 1–1 项 · 共 1 项", 20, 1)
	p.Key(ui.Cmd, ui.KeyA)
	p.Type("id:nonexistent")
	assertPage("共 0 项", 20, 0)
	p.Key(ui.Cmd, ui.KeyA)
	p.Key(0, ui.KeyBackspace)
	p.Key(0, ui.KeyEscape)
	click(t, p, "下一页", "截止日")
	assertPage("第 1–20 项 · 共 21 项", 20, 20)
	click(t, p, "每页数量", "50")
	assertPage("第 1–21 项 · 共 21 项", 50, 21)
	click(t, p, "每页数量", "100")
	assertPage("第 1–21 项 · 共 21 项", 100, 21)
}

func TestTaskKeyboardNavigationCrossesPagesAndReturnsFromDetail(t *testing.T) {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	m.navigate(tasksPage)
	p := ui.NewTester(m.view, 1190, 730)
	rows := m.taskRows()
	assertSelection := func(page, position int) {
		t.Helper()
		if m.taskPageIndex != page || m.taskSelection != m.objects[rows[position]].ID {
			t.Fatalf("want position %d on page %d; got page %d with selection %q", position, page+1, m.taskPageIndex+1, m.taskSelection)
		}
	}
	click(t, p, "下一页")
	p.Key(0, ui.KeyJ)
	assertSelection(1, 10)
	p.Key(0, ui.KeyK)
	assertSelection(0, 9)
	p.Key(0, ui.KeyJ)
	assertSelection(1, 10)
	p.Key(0, ui.KeyEnter)
	if !m.fullTask {
		t.Fatal("Enter should open the selected task")
	}
	p.Key(0, ui.KeyEscape)
	assertSelection(1, 10)
}

func TestAutomaticTaskPageSizeUsesListHeight(t *testing.T) {
	m := newMockup()
	m.objects = append(m.objects, loadExtendedTasks()...)
	m.useAdaptiveLayout()
	p := ui.NewTester(m.view, 1190, 830)
	wideSize := m.taskPageSize
	p.Key(ui.Shift, ui.KeyG)
	last := m.taskRows()[len(m.objects)-1]
	p.SetSize(1190, 620)
	if m.taskPageSize >= wideSize || m.taskPageSize <= 1 {
		t.Fatalf("shortening the window should reduce the page from %d, got %d", wideSize, m.taskPageSize)
	}
	page := m.pagedTaskRows(m.taskRows())
	if len(page) == 0 || page[len(page)-1] != last || m.taskSelection != m.objects[last].ID {
		t.Fatal("resizing should keep the last selected task on the visible page")
	}

	click(t, p, "每页数量", "20")
	p.SetSize(940, 700)
	if !m.layout.stacked || m.taskPageSize != 20 {
		t.Fatal("manual page size should survive an adaptive layout change")
	}
	click(t, p, "每页数量", "自动")
	shortSize := m.taskPageSize
	if shortSize >= 20 {
		t.Fatal("switching back to automatic should fit the stacked list")
	}
	p.SetSize(1190, 830)
	if m.taskPageSize != wideSize {
		t.Fatalf("expected the original auto fit after restoring height, got %d instead of %d", m.taskPageSize, wideSize)
	}
	m.taskGrouping = "按状态分组"
	p.Frame()
	if m.taskPageSize >= wideSize {
		t.Fatal("automatic pagination should reserve room for group headings")
	}
}
