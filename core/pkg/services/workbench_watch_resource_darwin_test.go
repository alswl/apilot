package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/dal"
	"github.com/alswl/apilot/core/pkg/integrations"
	"golang.org/x/sys/unix"
)

func TestWorkbenchPeriodicScanWhenWatcherExhausted(t *testing.T) {
	if os.Getenv("APILOT_WATCH_EXHAUSTION_TEST") != "1" {
		t.Skip("set APILOT_WATCH_EXHAUSTION_TEST=1 to test descriptor exhaustion")
	}
	vault := t.TempDir()
	for i := range 80 {
		dir := filepath.Join(vault, fmt.Sprintf("folder-%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for j := range 2 {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note-%d.md", j)), []byte(fmt.Sprintf("- [ ] Before 🆔 task-%d-%d\n", i, j)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	var original unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &original); err != nil {
		t.Fatal(err)
	}
	limited := original
	limited.Cur = 128
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &limited); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &original); err != nil {
			t.Errorf("restore descriptor limit: %v", err)
		}
	}()
	conn := common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault, Scope: "."}
	if err := integrations.WatchWorkbenchObsidian(context.Background(), conn, func(string) {}); err == nil {
		t.Fatal("expected watcher to fail under descriptor pressure")
	}
	s, err := NewWithWorkbenchOptions(t.TempDir(), func(path string) (Store, error) { return dal.Open(path) }, BuiltinWorkbenchPluginRegistry(), ObsidianSyncOptions{ScanInterval: time.Second, MaxRetries: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.SaveWorkbench(conn)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.LastSuccess != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(1200 * time.Millisecond)
	current, err := s.WorkbenchSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	covered := s.workbenchIdentityCoverageLocked(current.WorkbenchConnection, "folder-000/note-0.md")
	s.mu.Unlock()
	if covered {
		t.Fatal("failed watch was treated as continuous identity coverage")
	}
	note := filepath.Join(vault, "folder-000", "note-0.md")
	if err := os.WriteFile(note, []byte("- [x] After 🆔 task-0-0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range page.Items {
			if task.NativeID == "task-0-0" && task.Title == "After" && task.State == "done" {
				snapshot, err := s.WorkbenchSnapshot(id)
				if err != nil || len(snapshot.Attempts) == 0 || snapshot.Attempts[0].Trigger != "periodic" {
					t.Fatalf("change was not confirmed by periodic scan: %+v %v", snapshot.Attempts, err)
				}
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("periodic scan did not recover the change")
}
