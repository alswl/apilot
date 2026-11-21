package integrations

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchObsidianWatchAtomicSaveAndNewDirectory(t *testing.T) {
	vault := t.TempDir()
	scope := filepath.Join(vault, "notes")
	if err := os.Mkdir(scope, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	changed := make(chan string, 128)
	done := make(chan error, 1)
	go func() {
		done <- WatchWorkbenchObsidian(ctx, common.WorkbenchConnection{Vault: vault, Scope: "notes"}, func(path string) {
			select {
			case changed <- path:
			default:
			}
		})
	}()
	wait := func(path string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case actual := <-changed:
				if actual == path {
					return
				}
			case err := <-done:
				t.Fatalf("watch stopped: %v", err)
			case <-deadline:
				t.Fatalf("timed out waiting for %q", path)
			}
		}
	}
	wait("") // Initial coverage has been installed.
	path := filepath.Join(scope, "task.md")
	temp := filepath.Join(scope, ".task.tmp")
	if err := os.WriteFile(temp, []byte("- [ ] task\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp, path); err != nil {
		t.Fatal(err)
	}
	wait("notes/task.md")
	if err := os.Mkdir(filepath.Join(scope, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	wait("notes/nested") // The directory watch is installed before this hint.
	if err := os.WriteFile(filepath.Join(scope, "nested", "task.md"), []byte("- [ ] nested\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wait("notes/nested/task.md")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestWorkbenchObsidianWatchLosesRenamedDirectory(t *testing.T) {
	vault := t.TempDir()
	nested := filepath.Join(vault, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- WatchWorkbenchObsidian(ctx, common.WorkbenchConnection{Vault: vault, Scope: "."}, func(path string) {
			if path == "" {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		})
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("watch failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not start")
	}
	if err := os.Rename(nested, filepath.Join(vault, "renamed")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("lost watch was reported as success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("directory rename did not invalidate coverage")
	}
}

func TestWorkbenchObsidianWatchVaultAlias(t *testing.T) {
	parent := t.TempDir()
	vault := filepath.Join(parent, "vault")
	alias := filepath.Join(parent, "alias")
	if err := os.Mkdir(vault, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vault, alias); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan string, 8)
	done := make(chan error, 1)
	go func() {
		done <- WatchWorkbenchObsidian(ctx, common.WorkbenchConnection{Vault: alias, Scope: "."}, func(path string) {
			select {
			case events <- path:
			default:
			}
		})
	}()
	select {
	case <-events:
	case err := <-done:
		t.Fatalf("watch failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not start")
	}
	if err := os.WriteFile(filepath.Join(vault, "task.md"), []byte("- [ ] Task\n"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-events:
		if path != "task.md" {
			t.Fatalf("event path: %q", path)
		}
	case err := <-done:
		t.Fatalf("watch failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("missing event")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
