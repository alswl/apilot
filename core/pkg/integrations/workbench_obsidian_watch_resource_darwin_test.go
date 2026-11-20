package integrations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"golang.org/x/sys/unix"
)

// This opt-in measurement records kqueue descriptors for a reproducible vault.
func TestWorkbenchObsidianWatchResourceMeasurement(t *testing.T) {
	if os.Getenv("APILOT_WATCH_RESOURCE_TEST") != "1" {
		t.Skip("set APILOT_WATCH_RESOURCE_TEST=1 to measure watcher resources")
	}
	if _, err := exec.LookPath("lsof"); err != nil {
		t.Skip("lsof unavailable")
	}
	const directories = 200
	const notesPerDirectory = 2
	vault := t.TempDir()
	for i := range directories {
		dir := filepath.Join(vault, fmt.Sprintf("folder-%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for j := range notesPerDirectory {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note-%d.md", j)), []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	countFD := func() int {
		t.Helper()
		out, err := exec.Command("lsof", "-a", "-p", fmt.Sprint(os.Getpid()), "-d", "0-99999", "-Ff").Output()
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "f") {
				count++
			}
		}
		return count
	}
	beforeFD, beforeGoroutines := countFD(), runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	start := time.Now()
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
		cancel()
		t.Fatalf("watch failed: %v", err)
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("watch setup timed out")
	}
	setup := time.Since(start)
	duringFD, duringGoroutines := countFD(), runtime.NumGoroutine()
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	afterFD := countFD()
	t.Logf("directories=%d notes=%d setup=%s fd_before=%d fd_during=%d fd_after=%d goroutines_before=%d goroutines_during=%d", directories+1, directories*notesPerDirectory, setup.Round(time.Millisecond), beforeFD, duringFD, afterFD, beforeGoroutines, duringGoroutines)
	if afterFD > beforeFD+1 {
		t.Fatalf("watch descriptors did not close: before=%d after=%d", beforeFD, afterFD)
	}
}

func TestWorkbenchObsidianWatchReportsDescriptorExhaustion(t *testing.T) {
	if os.Getenv("APILOT_WATCH_EXHAUSTION_TEST") != "1" {
		t.Skip("set APILOT_WATCH_EXHAUSTION_TEST=1 and a low descriptor limit")
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
	vault := t.TempDir()
	for i := range 80 {
		dir := filepath.Join(vault, fmt.Sprintf("folder-%03d", i))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for j := range 2 {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note-%d.md", j)), []byte("- [ ] Task\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var installed bool
	err := WatchWorkbenchObsidian(ctx, common.WorkbenchConnection{Vault: vault, Scope: "."}, func(path string) {
		if path == "" {
			installed = true
		}
	})
	if err == nil || installed {
		t.Fatalf("exhausted watcher must report incomplete coverage: installed=%v err=%v", installed, err)
	}
	t.Logf("descriptor exhaustion correctly reports lost coverage: %v", err)
}
