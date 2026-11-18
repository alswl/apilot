package workbench

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alswl/apilot/pkg/config"
	"github.com/egoist/mygo/ui"
)

func TestCommandPaletteOpensTaskAndRestoresSelection(t *testing.T) {
	m := newMockup()
	p := ui.NewTester(m.view, 860, 580)
	p.Key(ui.Cmd, ui.KeyK)
	if !m.keyboard.palette || !p.Focused("搜索命令或任务") {
		t.Fatal("command palette did not focus its search field")
	}
	p.Type("example/payments#40")
	p.Key(0, ui.KeyEnter)
	if m.keyboard.palette || !m.inspector || m.objects[m.selected].NativeID != "40" {
		t.Fatal("Return must open the matching task by source ID")
	}
	p.Key(0, ui.KeyEscape)
	if m.inspector || m.taskSelection != "deploy" {
		t.Fatal("Escape must leave the opened row selected")
	}
	p.Key(0, ui.KeyUp)
	if m.taskSelection == "deploy" {
		t.Fatal("keyboard focus must return to the task table")
	}
}

func TestConfigurableSequencesRespectTextFocus(t *testing.T) {
	m := newMockup()
	settings := config.Keybindings{EnableSingleKeys: true, Bindings: map[string][]string{
		"command.palette": {"Cmd+Shift+P"}, "navigation.tasks": {"g t", "Cmd+2"},
	}}
	var err error
	m.keyboard.bindings, err = compileBindings(settings)
	if err != nil {
		t.Fatal(err)
	}
	p := ui.NewTester(m.view, 1190, 730)
	p.Key(ui.Cmd, ui.KeyK)
	if m.keyboard.palette {
		t.Fatal("overridden default still opens the palette")
	}
	p.Key(ui.Cmd|ui.Shift, ui.KeyP)
	if !m.keyboard.palette {
		t.Fatal("configured shortcut didn't open the palette")
	}
	p.Key(0, ui.KeyEscape)
	p.Key(0, ui.KeyG)
	p.Key(0, ui.KeyZ)
	p.Key(0, ui.KeyT)
	if m.page != nowPage {
		t.Fatal("an intervening key must cancel the prefix")
	}
	p.Key(0, ui.KeyG)
	p.Key(0, ui.KeyT)
	if m.page != tasksPage {
		t.Fatal("navigation sequence didn't open Tasks")
	}
	p.Key(ui.Cmd, ui.KeyF)
	if !p.Focused("搜索任务") {
		t.Fatal("find must focus the task search")
	}
	p.TypeKey(0, ui.KeyG, "g")
	p.TypeKey(0, ui.KeyN, "n")
	if m.page != tasksPage || m.search != "gn" {
		t.Fatalf("plain shortcuts must leave text input alone, page=%d input=%q", m.page, m.search)
	}
}

func TestInvalidReloadKeepsEffectiveBindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keybindings.json")
	k := defaultKeyboard()
	k.path = path
	write := func(value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"bindings":{"command.palette":["Cmd+Shift+P"],"shortcuts.help":[]}}`)
	k.reload()
	if k.loadError != "" || k.display("command.palette") != "Cmd+Shift+P" || k.display("shortcuts.help") != "" {
		t.Fatalf("valid overrides and unbinding should apply, got %q", k.loadError)
	}
	before := slices.Clone(k.bindings)
	write(`{"enableSingleKeys":true,"bindings":{"task.watch":["g"]}}`)
	k.reload()
	if k.loadError == "" || !slices.Equal(k.bindings, before) {
		t.Fatal("a prefix conflict must keep the previously valid configuration")
	}
}

func TestPaletteArrowMovesSearchResult(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	p := ui.NewTester(m.view, 1190, 730)
	p.Key(ui.Cmd, ui.KeyK)
	p.Type("退款")
	p.Key(0, ui.KeyDown)
	if m.keyboard.cursor != 1 {
		t.Fatalf("arrow should select the second result, got %d", m.keyboard.cursor)
	}
	p.Key(0, ui.KeyEnter)
	if !m.inspector || m.objects[m.selected].ID != "reply" {
		t.Fatal("Return must open the highlighted result")
	}
}

func TestKeyboardHelpUsesEffectiveBindings(t *testing.T) {
	m := newMockup()
	var err error
	m.keyboard.bindings, err = compileBindings(config.Keybindings{
		Bindings: map[string][]string{"command.palette": {"Cmd+Shift+P"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	p := ui.NewTester(m.view, 860, 580)
	p.Key(ui.Cmd, ui.KeySlash)
	if !m.keyboard.help || !slices.Contains(p.Texts(), "Cmd+Shift+P") {
		t.Fatal("shortcut help should render the effective configuration")
	}
	p.Key(0, ui.KeyEscape)
	if m.keyboard.palette {
		t.Fatal("escape should dismiss shortcut help")
	}
}

func TestSingleKeysMoveTaskSelection(t *testing.T) {
	m := newMockup()
	m.keyboard.bindings, _ = compileBindings(config.Keybindings{EnableSingleKeys: true})
	m.navigate(tasksPage)
	p := ui.NewTester(m.view, 1190, 730)
	p.Key(0, ui.KeyJ)
	if m.taskSelection != "reply" {
		t.Fatalf("j should select the first task, got %q", m.taskSelection)
	}
	p.Key(0, ui.KeyJ)
	if m.taskSelection != "deploy" {
		t.Fatalf("j should advance to the second task, got %q", m.taskSelection)
	}
}

func TestVimModeMovesToBoundariesAndLeavesSearch(t *testing.T) {
	m := newMockup()
	var err error
	m.keyboard.bindings, err = compileBindings(config.Keybindings{Mode: "vim"})
	if err != nil {
		t.Fatal(err)
	}
	m.keyboard.vim = true
	m.navigate(tasksPage)
	p := ui.NewTester(m.view, 1190, 730)
	rows := m.taskRows()
	p.Key(ui.Shift, ui.KeyG)
	if m.taskSelection != m.objects[rows[len(rows)-1]].ID {
		t.Fatal("G should move to the last visible task")
	}
	p.Key(0, ui.KeyG)
	p.Key(0, ui.KeyG)
	if m.taskSelection != m.objects[rows[0]].ID {
		t.Fatal("gg should move to the first visible task")
	}
	p.Key(0, ui.KeyI)
	if !p.Focused("搜索任务") {
		t.Fatal("i should focus the search field")
	}
	p.TypeKey(0, ui.KeyJ, "j")
	if m.search != "j" {
		t.Fatal("j must type into the search field")
	}
	p.Key(0, ui.KeyEscape)
	if p.Focused("搜索任务") || m.search != "j" {
		t.Fatal("Escape should leave search and preserve the filter")
	}
}

func TestEnglishWorkbenchPreservesCommandAndFixtureIDs(t *testing.T) {
	englishUI.Store(true)
	t.Cleanup(func() { englishUI.Store(false) })
	m := newMockup()
	p := ui.NewTester(m.view, 1190, 730)
	p.Key(ui.Cmd, ui.KeyK)
	if !p.Focused("Search commands or tasks") {
		t.Fatal("expected an English command palette")
	}
	p.Type("legacy order")
	p.Key(0, ui.KeyEnter)
	if !m.inspector || m.objects[m.selected].ID != "refund" {
		t.Fatal("English search must open the same fixture task")
	}
	click(t, p, "Edit due date")
	if !m.editingDue || m.dueDraft != "11 月 20 日" {
		t.Fatal("localized editing must retain the canonical date value")
	}
}

func TestNumberedShortcutsFollowSidebarOrder(t *testing.T) {
	m := newMockup()
	p := ui.NewTester(m.view, 860, 580)
	for _, step := range []struct {
		key  ui.Key
		page int
	}{
		{ui.Key2, tasksPage}, {ui.Key3, connectionsPage}, {ui.Key4, scheduledPage},
		{ui.Key5, usagePage}, {ui.Key1, nowPage}, {ui.Key9, usagePage},
	} {
		p.Key(ui.Cmd, step.key)
		if m.page != step.page {
			t.Fatalf("Cmd+%v went to page %d, want %d", step.key, m.page, step.page)
		}
	}
}

func TestVimNewTaskKeyboardFlow(t *testing.T) {
	m := newMockup()
	m.keyboard.reload()
	if !m.keyboard.vim {
		t.Fatal("a workbench without a custom config must start in Vim mode")
	}

	p := ui.NewTester(m.view, 860, 580)
	p.Key(0, ui.KeySlash)
	if !p.Focused("搜索任务") {
		t.Fatal("/ should open and focus the task search")
	}
	p.TypeKey(0, ui.KeyN, "n")
	if m.newTaskOpen || m.search != "n" {
		t.Fatal("n must type while search is focused")
	}
	p.Key(0, ui.KeyEscape)
	p.Key(0, ui.KeyN)
	if !m.newTaskOpen || !p.Focused("任务名称") {
		t.Fatal("n should open and focus new task")
	}
	p.Type("取消的新任务")
	p.Key(0, ui.KeyEscape)
	if m.newTaskOpen || len(m.objects) != 3 {
		t.Fatal("Escape must close the dialog without creating a task")
	}
	p.Key(0, ui.KeyN)
	p.Type("核对测试订单")
	p.Key(0, ui.KeyEnter)
	if m.newTaskOpen || len(m.objects) != 4 || !m.inspector {
		t.Fatal("Enter must create a task and open it")
	}
	task := m.objects[m.selected]
	if task.Title != "核对测试订单" || task.ConnectionID != "vault" || m.search != "" {
		t.Fatalf("wrong task, source or visibility after create: %+v", task)
	}
	p.Key(0, ui.KeyEscape)
	if m.inspector || m.taskSelection != task.ID {
		t.Fatal("Escape must return to the created task in the list")
	}
}

func TestVimMovesTasksFromDetailsAndOutsideTableFocus(t *testing.T) {
	for _, width := range []int{860, 1190} {
		m := newMockup()
		p := ui.NewTester(m.view, width, 730)
		p.Key(ui.Cmd, ui.KeyK)
		p.Type("refund-check")
		p.Key(0, ui.KeyEnter)
		if !m.inspector || m.objects[m.selected].ID != "refund" {
			t.Fatal("palette should open the refund detail")
		}
		p.Key(0, ui.KeyK)
		if !m.inspector || m.objects[m.selected].ID != "deploy" {
			t.Fatalf("%d pt: k should browse previous task from the inspector", width)
		}
		p.Key(0, ui.KeyJ)
		if !m.inspector || m.objects[m.selected].ID != "refund" {
			t.Fatalf("%d pt: j should browse next task from the inspector", width)
		}
		p.Key(0, ui.KeyEscape)
		if m.inspector || m.taskSelection != "refund" {
			t.Fatal("Escape should return to the task that was last open")
		}
	}
}
