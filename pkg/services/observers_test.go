package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func TestObserverIsolationAndRecovery(t *testing.T) {
	s, e := openService(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	mode := filepath.Join(t.TempDir(), "state")
	os.WriteFile(mode, []byte("bad"), 0600)
	in := common.ObserverInput{Name: "poll", Executable: "/bin/sh", Args: []string{"-c", "if [ \"$(cat \"$1\")\" = bad ]; then exit 7; fi; echo '{\"ok\":true}'", "fixture", mode}, Cwd: "/tmp", IntervalMS: 1000, TimeoutMS: 100, Enabled: true, OutputMode: "json"}
	v, e := s.SaveObserver("", in, 0)
	if e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		v, _ = s.Observer(v.ID)
		if v.Health == "unhealthy" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v.Health != "unhealthy" {
		t.Fatal(v)
	}
	task, e := s.SaveTask("", input("unaffected", "echo ok"), 0)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.RunTask(task.ID, "cli")
	if e != nil {
		t.Fatal(e)
	}
	if waitRun(t, s, r.ID).Status != "succeeded" {
		t.Fatal("observer blocked task")
	}
	os.WriteFile(mode, []byte("good"), 0600)
	deadline = time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		v, _ = s.Observer(v.ID)
		if v.Health == "healthy" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if v.Health != "healthy" {
		t.Fatal(v)
	}
	events, e := s.Events()
	if e != nil || len(events) != 2 {
		t.Fatal(events, e)
	}
	if len(s.Runs("")) != 1 {
		t.Fatal("observations polluted run history")
	}
}
func TestTerminalRunCannotBeOverwritten(t *testing.T) {
	s, e := openService(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	task, e := s.SaveTask("", input("once", "true"), 0)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.RunTask(task.ID, "cli")
	if e != nil {
		t.Fatal(e)
	}
	r = waitRun(t, s, r.ID)
	r.Status = "failed"
	if e = s.store.SaveRun(r); e == nil {
		t.Fatal("overwrote terminal result")
	}
}
func TestNotificationDedup(t *testing.T) {
	s, e := openService(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for i := 0; i < 20; i++ {
		allowed, e := s.AllowNotification("task1:failed")
		if e != nil || allowed != (i == 0) {
			t.Fatal(i, allowed, e)
		}
	}
}
