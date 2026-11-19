package integrations

import (
	"context"
	"os"
	"path/filepath"
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
