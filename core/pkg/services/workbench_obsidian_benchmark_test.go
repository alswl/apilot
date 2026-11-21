package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

// BenchmarkWorkbenchObsidianStatePreparation isolates identity verification
// and patch preparation. It does not include dispatch or receipt confirmation.
func BenchmarkWorkbenchObsidianStatePreparation(b *testing.B) {
	const directories, notesPerDirectory = 200, 20
	vault := b.TempDir()
	for i := range directories {
		dir := filepath.Join(vault, fmt.Sprintf("folder-%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			b.Fatal(err)
		}
		for j := range notesPerDirectory {
			content := fmt.Sprintf("# Project\nContext for the task.\n- [ ] Review item %d-%d #project 🆔 task-%d-%d\n", i, j, i, j)
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note-%02d.md", j)), []byte(content), 0600); err != nil {
				b.Fatal(err)
			}
		}
	}
	c := common.WorkbenchConnection{ID: "benchmark", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: ".", Version: 1}
	ctx := context.Background()
	tasks, err := integrations.ReadWorkbenchObsidianTasksInNote(ctx, c, "folder-100/note-10.md")
	if err != nil || len(tasks) != 1 {
		b.Fatalf("target: %d tasks, %v", len(tasks), err)
	}
	task := tasks[0]
	input := common.WorkbenchTaskWrite{RequestID: "benchmark", State: "done", TargetVersion: workbenchTaskTargetVersion(task)}
	for _, test := range []struct {
		name    string
		covered bool
	}{{"full_identity_check", false}, {"verified_note", true}} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_, _, _, conflict, err := prepareWorkbenchObsidianStateWithCoverage(ctx, common.WorkbenchSnapshot{WorkbenchConnection: c}, input, task, test.covered)
				if err != nil || conflict.SyncState != "" {
					b.Fatalf("prepare: %+v %v", conflict, err)
				}
			}
		})
	}
}
