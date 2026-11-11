package app

import (
	"github.com/alswl/apilot/internal/model"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetentionKeepsActiveAndUserDirectory(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	run := model.Run{ID: "old", Status: "succeeded", CreatedAt: old, LogPath: filepath.Join(s.dir, "logs", "old")}
	os.MkdirAll(run.LogPath, 0700)
	os.WriteFile(filepath.Join(run.LogPath, "stdout.log"), []byte("old"), 0600)
	s.mu.Lock()
	s.runs[run.ID] = run
	e = s.store.SaveRun(run)
	s.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Prune(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(run.LogPath); !os.IsNotExist(e) {
		t.Fatal("old log not removed")
	}
	if _, e = s.Run(run.ID); e == nil {
		t.Fatal("old run retained")
	}
	unsafe := model.Run{ID: "unsafe", Status: "failed", CreatedAt: old, LogPath: t.TempDir()}
	s.mu.Lock()
	s.runs[unsafe.ID] = unsafe
	s.mu.Unlock()
	if e = s.Prune(); e == nil {
		t.Fatal("accepted outside log path")
	}
	if _, e = os.Stat(unsafe.LogPath); e != nil {
		t.Fatal("deleted user directory")
	}
}
