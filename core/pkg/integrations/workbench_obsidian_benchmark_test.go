package integrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
)

// BenchmarkWorkbenchObsidianRead compares source-read scopes on the same
// synthetic vault; it does not measure dispatch, UI feedback or readback.
func BenchmarkWorkbenchObsidianRead(b *testing.B) {
	const directories, notesPerDirectory = 200, 20
	vault := b.TempDir()
	for i := range directories {
		dir := filepath.Join(vault, fmt.Sprintf("folder-%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			b.Fatal(err)
		}
		for j := range notesPerDirectory {
			path := filepath.Join(dir, fmt.Sprintf("note-%02d.md", j))
			content := fmt.Sprintf("# Project\nContext for the task.\n- [ ] Review item %d-%d #project 🆔 task-%d-%d\n", i, j, i, j)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				b.Fatal(err)
			}
		}
	}
	c := common.WorkbenchConnection{Vault: vault, Scope: "."}
	ctx := context.Background()
	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			tasks, err := ReadWorkbenchObsidian(ctx, c)
			if err != nil || len(tasks) != directories*notesPerDirectory {
				b.Fatalf("full read: %d tasks, %v", len(tasks), err)
			}
		}
	})
	b.Run("note", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			tasks, err := ReadWorkbenchObsidianTasksInNote(ctx, c, "folder-100/note-10.md")
			if err != nil || len(tasks) != 1 {
				b.Fatalf("note read: %d tasks, %v", len(tasks), err)
			}
		}
	})
}
