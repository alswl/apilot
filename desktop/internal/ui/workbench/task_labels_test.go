package workbench

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestGitHubLabelPickerSearchAndCancel(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	m.openFullTask(2)
	p := ui.NewTester(m.view, 1190, 730)
	p.Key(0, ui.KeyL)
	if m.labelEditor == nil || !p.Focused("搜索标签") {
		t.Fatal("l must open the label editor and focus search")
	}
	p.Type("bug")
	p.Key(0, ui.KeyEnter)
	if !slices.Contains(m.labelEditor.labels, "bug") || slices.Contains(m.objects[2].Labels, "bug") {
		t.Fatal("choosing a label must change the draft only")
	}
	click(t, p, "移除标签 refund")
	if slices.Contains(m.labelEditor.labels, "refund") {
		t.Fatal("removing one label should update the draft")
	}
	p.Key(0, ui.KeyEscape)
	if m.labelEditor != nil || !m.fullTask || !slices.Equal(m.objects[2].Labels, []string{"refund", "documentation"}) {
		t.Fatal("Escape should discard the draft and retain the task detail")
	}
}

func TestObsidianLabelPickerAddsNestedTag(t *testing.T) {
	m := newMockup()
	m.navigate(tasksPage)
	m.openFullTask(0)
	p := ui.NewTester(m.view, 860, 580)
	click(t, p, "管理标签")
	p.Type("#payments/incident")
	p.Key(0, ui.KeyEnter)
	if !slices.Contains(m.labelEditor.labels, "payments/incident") || slices.Contains(m.objects[0].Labels, "payments/incident") {
		t.Fatal("new tag must be normalized and remain uncommitted")
	}
}

func TestObsidianLabelSyntax(t *testing.T) {
	for _, invalid := range []string{"", "123", "refund ops", "foo//bar", "/foo", "foo,"} {
		if validObsidianLabel(invalid) {
			t.Errorf("invalid tag accepted: %q", invalid)
		}
	}
	for _, valid := range []string{"payments/refund", "tag-1", "待验收/第2批"} {
		if !validObsidianLabel(valid) {
			t.Errorf("valid tag rejected: %q", valid)
		}
	}
}
