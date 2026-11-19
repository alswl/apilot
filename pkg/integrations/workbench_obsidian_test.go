package integrations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alswl/apilot/pkg/common"
)

func TestWorkbenchObsidianScopeAndParse(t *testing.T) {
	vault := t.TempDir()
	work := filepath.Join(vault, "Work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	note := []byte("---\n- [ ] config\n---\n# Release\n- [ ] Review #release 📅 2026-11-20 🆔 accept1\n  2. [/] Validate\n~~~md\n- [ ] sample\n~~~\n<!--\n- [ ] hidden\n-->")
	path := filepath.Join(work, "Release.md")
	if err := os.WriteFile(path, note, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(vault, "Outside.md")
	if err := os.WriteFile(outside, []byte("- [ ] Outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(work, "link.md")); err != nil {
		t.Fatal(err)
	}
	c := common.WorkbenchConnection{Vault: vault, Scope: "Work", Statuses: map[string]string{"/": "in_progress"}}
	tasks, err := ReadWorkbenchObsidian(context.Background(), c)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks=%+v err=%v", tasks, err)
	}
	first := tasks[0]
	if first.SourceKey != "task:accept1" || first.NativeFields["due"] != "2026-11-20" || first.Locator != "Work/Release.md:5" || tasks[1].State != "in_progress" {
		t.Fatalf("%+v", tasks)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != string(note) {
		t.Fatal("note changed")
	}
	c.Scope = "../"
	if _, err := ReadWorkbenchObsidian(context.Background(), c); err == nil {
		t.Fatal("accepted scope escape")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Scope = "Work"
	if _, err := ReadWorkbenchObsidian(ctx, c); err == nil {
		t.Fatal("accepted cancelled scan")
	}
}

func TestWorkbenchObsidianHeadingTargets(t *testing.T) {
	vault := t.TempDir()
	work := filepath.Join(vault, "Work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	note := []byte("---\n# Metadata\n---\n# Customer A\n## Tasks\n~~~\n# Example\n~~~\n# Customer B\n## Tasks ###\n### C#\n# Duplicate\n## Nested\n# Duplicate\n")
	if err := os.WriteFile(filepath.Join(work, "Empty.md"), note, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "Outside.md"), note, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Outside.md", filepath.Join(work, "Alias.md")); err != nil {
		t.Fatal(err)
	}
	connection := common.WorkbenchConnection{Vault: vault, Scope: "Work"}
	notes, err := ListWorkbenchObsidianNotes(context.Background(), connection)
	if err != nil || len(notes) != 1 || notes[0].NotePath != "Work/Empty.md" {
		t.Fatalf("notes: %+v %v", notes, err)
	}
	for _, path := range []string{"Outside.md", "Work/../Outside.md", "Work/Alias.md"} {
		if _, err := ReadWorkbenchObsidianNote(connection, path); err == nil {
			t.Fatalf("read out-of-scope target %s", path)
		}
	}
	data, err := ReadWorkbenchObsidianNote(connection, notes[0].NotePath)
	if err != nil {
		t.Fatal(err)
	}
	headings := WorkbenchObsidianHeadings(data)
	if len(headings) != 8 || headings[3].HeadingPath[0] != "Customer B" ||
		headings[3].HeadingPath[1] != "Tasks" || headings[4].HeadingPath[2] != "C#" ||
		headings[3].TargetVersion == "" || headings[6].TargetVersion != "" || headings[6].Reason == "" {
		t.Fatalf("headings: %+v", headings)
	}
}

func TestWorkbenchObsidianInsertsInDirectHeadingBody(t *testing.T) {
	content := []byte("\xef\xbb\xbf# A\r\n## Tasks\r\n\r\n### Child\r\n- [ ] Existing\r\n# B\r\n## Tasks\r\n")
	headings := WorkbenchObsidianHeadings(content)
	result, err := InsertWorkbenchObsidianTask(content, []string{"A", "Tasks"}, headings[1].TargetVersion, "New task", "apilot-test")
	if err != nil {
		t.Fatal(err)
	}
	want := "\xef\xbb\xbf# A\r\n## Tasks\r\n- [ ] New task 🆔 apilot-test\r\n\r\n### Child\r\n- [ ] Existing\r\n# B\r\n## Tasks\r\n"
	if string(result) != want {
		t.Fatalf("result:\n%q\nwant:\n%q", result, want)
	}
	if _, err := InsertWorkbenchObsidianTask(result, []string{"A", "Tasks"}, headings[1].TargetVersion, "New task", "apilot-test"); err == nil {
		t.Fatal("accepted stale heading version")
	}
}

func TestWorkbenchObsidianCheckboxKeepsOtherBytes(t *testing.T) {
	connection := common.WorkbenchConnection{}
	original := "\xef\xbb\xbf# Tasks\r\n  2. [ ] Ship #release 📅 2026-11-20 🆔 release\r\n- [ ] Keep\r\n"
	task := parseWorkbenchNote(connection, "Release.md", original)[0]
	edited := "Extra paragraph\r\n" + original
	data, err := SetWorkbenchObsidianCheckbox(connection, []byte(edited), task, "done")
	want := strings.Replace(edited, "2. [ ]", "2. [x]", 1)
	if err != nil || string(data) != want {
		t.Fatalf("result: %q, %v", data, err)
	}
	duplicate := original + "  2. [ ] Ship #release 📅 2026-11-20 🆔 release\r\n"
	if _, err := SetWorkbenchObsidianCheckbox(connection, []byte(duplicate), task, "done"); err == nil {
		t.Fatal("accepted a duplicate target")
	}
}

func TestWorkbenchObsidianTaskMetadataFormats(t *testing.T) {
	content := "# Release\n" +
		"- [ ] Ship 🔁 every week on Monday 📅 2026-11-23 ➕ 2026-11-19 ❌ 2026-11-25 ⛔ one,two 🏁 delete ⏫ 🆔 ship\n" +
		"- [x] Review [created:: 2026-11-19] (start:: 2026-11-20) [scheduled:: 2026-11-22] [completion:: 2026-11-24] [repeat:: every day] [dependsOn:: ship] [id:: review] [priority:: lowest]\n"
	tasks := parseWorkbenchNote(common.WorkbenchConnection{}, "Projects/Release.md", content)
	if len(tasks) != 2 || tasks[0].Title != "Ship" || tasks[1].Title != "Review" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	for index, fields := range []map[string]string{
		{"file": "Release.md", "path": "Projects/Release.md", "section": "Release", "line": "2",
			"recurrence": "every week on Monday", "due": "2026-11-23", "created": "2026-11-19",
			"cancelled": "2026-11-25", "depends_on": "one,two", "on_completion": "delete", "priority": "high", "id": "ship"},
		{"created": "2026-11-19", "start": "2026-11-20", "scheduled": "2026-11-22", "done": "2026-11-24",
			"recurrence": "every day", "priority": "lowest", "depends_on": "ship", "id": "review"},
	} {
		for field, want := range fields {
			if got := tasks[index].NativeFields[field]; got != want {
				t.Errorf("task %d field %s = %q, want %q", index, field, got, want)
			}
		}
	}
}

func TestWorkbenchObsidianCheckboxDistinguishesSections(t *testing.T) {
	connection := common.WorkbenchConnection{}
	original := "# First\n- [ ] Review\n# Second\n- [ ] Review\n"
	tasks := parseWorkbenchNote(connection, "Release.md", original)
	result, err := SetWorkbenchObsidianCheckbox(connection, []byte(original), tasks[1], "done")
	if err != nil || string(result) != "# First\n- [ ] Review\n# Second\n- [x] Review\n" {
		t.Fatalf("second section: %q %v", result, err)
	}
}
