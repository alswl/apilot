package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchWatchTriggersSourceRefresh(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	note := filepath.Join(vault, "tasks.md")
	if err := os.WriteFile(note, []byte("- [ ] First 🆔 first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "watched", Kind: common.WorkbenchObsidianTasks, Name: "Watched", Vault: vault, Scope: ".", Version: 1}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	waitFor := func(title string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) == 1 && page.Items[0].Title == title {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("source did not converge to %q", title)
	}
	waitFor("First")
	if err := os.WriteFile(note, []byte("- [ ] Second 🆔 first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFor("Second")
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("task: %+v %v", page, err)
	}
	taskID := page.Items[0].ID
	if err := s.SetWorkbenchLabels(taskID, []string{"local"}); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(vault, "renamed.md")
	if err := os.Rename(note, moved); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err = s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 && page.Items[0].NativeFields["path"] == "renamed.md" {
			if page.Items[0].ID != taskID || len(page.Items[0].WorkbenchLabels) != 1 {
				t.Fatalf("identity or metadata changed: %+v", page.Items[0])
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(page.Items) != 1 || page.Items[0].NativeFields["path"] != "renamed.md" {
		t.Fatal("renamed note did not converge")
	}
	if err := os.WriteFile(moved, []byte("- [ ] Second 🆔 first\n- [ ] Duplicate 🆔 first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := s.WorkbenchSnapshot(conn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].State == "failed" {
			page, err = s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
			if err != nil || len(page.Items) != 1 || page.Items[0].ID != taskID {
				t.Fatalf("ambiguous source replaced prior task: %+v %v", page, err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("duplicate source identity was not reported")
}

func TestWorkbenchWatchRecoversAfterScopeDisappears(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store, err := s.workbenchStore()
	if err != nil {
		t.Fatal(err)
	}
	vault := t.TempDir()
	scope := filepath.Join(vault, "notes")
	moved := filepath.Join(vault, "offline")
	if err := os.Mkdir(scope, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scope, "task.md"), []byte("- [ ] Before 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	conn := common.WorkbenchConnection{ID: "scope", Name: "Scope", Kind: common.WorkbenchObsidianTasks, Vault: vault, Scope: "notes", Version: 1}
	if err := store.SaveWorkbench(conn, false); err != nil {
		t.Fatal(err)
	}
	wait := func(title string) common.WorkbenchTask {
		t.Helper()
		deadline := time.Now().Add(7 * time.Second)
		for time.Now().Before(deadline) {
			page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) == 1 && page.Items[0].Title == title {
				return page.Items[0]
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("source did not converge to %s", title)
		return common.WorkbenchTask{}
	}
	before := wait("Before")
	confirmed, err := s.WorkbenchSnapshot(conn.ID)
	if err != nil || confirmed.LastSuccess == nil {
		t.Fatalf("initial confirmation: %+v %v", confirmed, err)
	}
	if err := os.Rename(scope, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moved, "task.md"), []byte("- [ ] After 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(7 * time.Second)
	failed := false
	for time.Now().Before(deadline) {
		snapshot, err := s.WorkbenchSnapshot(conn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].State == "failed" {
			if snapshot.LastSuccess == nil || !snapshot.LastSuccess.Equal(*confirmed.LastSuccess) {
				t.Fatalf("offline read advanced confirmation: %+v", snapshot)
			}
			failed = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !failed {
		t.Fatal("offline scope was not reported")
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: conn.ID})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != before.ID {
		t.Fatalf("offline lost confirmed data: %+v %v", page, err)
	}
	if err := os.Rename(moved, scope); err != nil {
		t.Fatal(err)
	}
	after := wait("After")
	if after.ID != before.ID {
		t.Fatalf("identity changed: %s -> %s", before.ID, after.ID)
	}
}

func TestWorkbenchWatchPauseAndResume(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vault := t.TempDir()
	note := filepath.Join(vault, "task.md")
	if err := os.WriteFile(note, []byte("- [ ] Before 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial read did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := s.PauseWorkbench(id, snapshot.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(note, []byte("- [ ] After 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Before" {
		t.Fatalf("paused source changed: %+v %v", page, err)
	}
	if _, err := s.PauseWorkbench(id, paused.Version, false); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err = s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 && page.Items[0].Title == "After" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resume did not read source changes")
}

func TestWorkbenchWatchScopeChangeRemovesOldVisibility(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vault := t.TempDir()
	for _, dir := range []string{"Old", "New"} {
		if err := os.Mkdir(filepath.Join(vault, dir), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vault, dir, "task.md"), []byte("- [ ] "+dir+" 🆔 "+dir+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault, Scope: "Old"})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(title string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) == 1 && page.Items[0].Title == title {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("scope did not converge to %s", title)
	}
	wait("Old")
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	c := snapshot.WorkbenchConnection
	c.Scope = "New"
	if _, err := s.SaveWorkbench(c); err != nil {
		t.Fatal(err)
	}
	wait("New")
	if err := os.WriteFile(filepath.Join(vault, "Old", "task.md"), []byte("- [ ] Old changed 🆔 Old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "New" {
		t.Fatalf("old scope remained visible: %+v %v", page, err)
	}
}

func TestWorkbenchWatchConfirmsSingleNoteWithoutFullRead(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vault := t.TempDir()
	note := filepath.Join(vault, "task.md")
	if err := os.WriteFile(note, []byte("- [ ] Before 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		active := s.workbenchActive[id] != nil
		s.mu.Unlock()
		if snapshot.LastSuccess != nil && !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial read did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Wait for the subscription's full verification rather than guessing its duration.
	deadline = time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		watch := s.workbenchWatchReady[id]
		ready := watch.installed && watch.verified && s.workbenchActive[id] == nil && !s.workbenchDirty[id] && len(s.workbenchPaths[id]) == 0
		s.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscription did not establish verified coverage")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before, err := s.WorkbenchSnapshot(id)
	if err != nil || before.LastSuccess == nil {
		t.Fatalf("initial snapshot: %+v %v", before, err)
	}
	s.mu.Lock()
	s.workbenchReader = func(context.Context, common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		return nil, errors.New("full read unavailable")
	}
	s.mu.Unlock()
	if err := os.WriteFile(note, []byte("- [x] After 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 && page.Items[0].Title == "After" && page.Items[0].State == "done" {
			after, err := s.WorkbenchSnapshot(id)
			if err != nil || after.LastSuccess == nil || !after.LastSuccess.Equal(*before.LastSuccess) {
				t.Fatalf("single note advanced full confirmation: %+v %v", after, err)
			}
			if len(after.Attempts) == 0 || after.Attempts[0].State == "running" {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			attempt := after.Attempts[0]
			if attempt.State != "success" || attempt.Scope != "notes" || attempt.Coverage != "complete" ||
				len(attempt.Paths) != 1 || attempt.Paths[0] != "task.md" || attempt.ConnectionVersion != before.Version {
				t.Fatalf("single note attempt lacks its coverage: %+v", attempt)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	snapshot, _ := s.WorkbenchSnapshot(id)
	s.mu.Lock()
	watch := s.workbenchWatchReady[id]
	dirty, paths, active := s.workbenchDirty[id], s.workbenchPaths[id], s.workbenchActive[id] != nil
	s.mu.Unlock()
	t.Fatalf("single-note change did not converge while full read was unavailable: snapshot=%+v watch=%+v dirty=%t paths=%v active=%t", snapshot, watch, dirty, paths, active)
}

func TestWorkbenchWatchQueuesNoteChangedDuringFullRead(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vault := t.TempDir()
	note := filepath.Join(vault, "task.md")
	if err := os.WriteFile(note, []byte("- [ ] Before 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		snapshot, err := s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		s.mu.Lock()
		active := s.workbenchActive[id] != nil
		s.mu.Unlock()
		if snapshot.LastSuccess != nil && !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial read did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Let the subscription finish its installation read before controlling one scan.
	time.Sleep(1200 * time.Millisecond)
	started, release := make(chan struct{}), make(chan struct{})
	var startedOnce sync.Once
	s.mu.Lock()
	s.workbenchReader = func(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		tasks, err := readWorkbenchObsidianWrittenVault(ctx, c)
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return tasks, err
	}
	s.mu.Unlock()
	if err := s.SyncWorkbench(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("full scan did not start")
	}
	reading, err := s.WorkbenchSnapshot(id)
	if err != nil || len(reading.Attempts) == 0 {
		t.Fatalf("reading snapshot: %+v %v", reading, err)
	}
	readingID := reading.Attempts[0].ID
	if err := os.WriteFile(note, []byte("- [x] After 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		queued := s.workbenchDirty[id] || s.workbenchPaths[id]["task.md"]
		s.mu.Unlock()
		if queued {
			break
		}
		if time.Now().After(deadline) {
			close(release)
			t.Fatal("watch did not queue the edit during the scan")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 && page.Items[0].Title == "After" && page.Items[0].State == "done" {
			snapshot, err := s.WorkbenchSnapshot(id)
			if err != nil {
				t.Fatal(err)
			}
			superseded := false
			for _, attempt := range snapshot.Attempts {
				if attempt.ID == readingID && attempt.State == "superseded" {
					superseded = true
				}
			}
			if !superseded {
				t.Fatalf("older scan was not superseded: %+v", snapshot.Attempts)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("queued edit was not applied after the older scan")
}

func TestWorkbenchInitialReadWaitsForWatchInstallation(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "task.md"), []byte("- [ ] First 🆔 first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	started := make(chan bool, 4)
	s.mu.Lock()
	s.workbenchReader = func(ctx context.Context, conn common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		s.mu.Lock()
		readiness := s.workbenchWatchReady[conn.ID]
		installed := readiness.version == conn.Version && readiness.signaled
		s.mu.Unlock()
		select {
		case started <- installed:
		default:
		}
		return readWorkbenchObsidianWrittenVault(ctx, conn)
	}
	s.mu.Unlock()
	if _, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault}); err != nil {
		t.Fatal(err)
	}
	select {
	case installed := <-started:
		if !installed {
			t.Fatal("initial source read started before watch installation completed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initial source read did not start")
	}
}

func TestWorkbenchCancelWhileWatchInstallationFails(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.mu.Lock()
	s.workbenchReader = func(ctx context.Context, _ common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	s.mu.Unlock()
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: t.TempDir(), Scope: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil || len(snapshot.Attempts) != 1 {
		t.Fatalf("initial attempt: %+v %v", snapshot, err)
	}
	attemptID := snapshot.Attempts[0].ID
	if _, err := s.CancelWorkbenchSync(id, attemptID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].State == "cancelled" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(snapshot.Attempts) != 1 || snapshot.Attempts[0].State != "cancelled" {
		t.Fatalf("cancelled attempt was replaced: %+v", snapshot.Attempts)
	}
	time.Sleep(1200 * time.Millisecond)
	snapshot, err = s.WorkbenchSnapshot(id)
	if err != nil || len(snapshot.Attempts) != 1 || snapshot.Attempts[0].State != "cancelled" {
		t.Fatalf("failed watch restarted a cancelled scan: %+v %v", snapshot.Attempts, err)
	}
}

func TestWorkbenchIdentityCoverageTracksWatchedPaths(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vault := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "task.md"), []byte("- [ ] Task 🆔 stable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	id, err := s.SaveWorkbench(common.WorkbenchConnection{Kind: common.WorkbenchObsidianTasks, Name: "Vault", Vault: vault})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	conn := snapshot.WorkbenchConnection
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		verified := s.workbenchIdentityCoverageLocked(conn, "task.md")
		s.mu.Unlock()
		if verified {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("full scan did not establish watched coverage")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.recordWorkbenchWatchEvent(id, conn.Version, "task.md")
	s.mu.Lock()
	targetStillCovered := s.workbenchIdentityCoverageLocked(conn, "task.md")
	otherCovered := s.workbenchIdentityCoverageLocked(conn, "other.md")
	s.mu.Unlock()
	if !targetStillCovered || otherCovered {
		t.Fatalf("wrong path coverage: target=%v other=%v", targetStillCovered, otherCovered)
	}
	s.recordWorkbenchWatchEvent(id, conn.Version, "other.md")
	s.mu.Lock()
	valid := s.workbenchIdentityCoverageLocked(conn, "task.md")
	s.mu.Unlock()
	if valid {
		t.Fatal("an unverified other note did not invalidate target identity coverage")
	}
	_ = s.signalWorkbenchWatchReady(id, conn.Version, false)
	s.mu.Lock()
	valid = s.workbenchIdentityCoverageLocked(conn, "task.md")
	s.mu.Unlock()
	if valid {
		t.Fatal("lost watcher retained identity coverage")
	}
}
