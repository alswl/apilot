package app

import (
	"github.com/alswl/apilot/internal/model"
	"github.com/alswl/apilot/internal/runner"
	"github.com/alswl/apilot/internal/storage"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		os.Exit(runner.Serve())
	}
	os.Exit(m.Run())
}
func input(name, cmd string) model.TaskInput {
	return model.TaskInput{Name: name, Executable: "/bin/sh", Args: []string{"-c", cmd}, Cwd: "/tmp", TimeoutMS: 5000, Enabled: true}
}
func waitRun(t *testing.T, s *Service, id string) model.Run {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		r, e := s.Run(id)
		if e != nil {
			t.Fatal(e)
		}
		if !r.Active() {
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run did not finish")
	return model.Run{}
}
func TestCLIReadyCoreAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	in := input("demo", "printf ok; printf fail >&2; exit 7")
	in.EnvSet = map[string]string{"KEY": "private"}
	task, e := s.SaveTask("", in, 0)
	if e != nil {
		t.Fatal(e)
	}
	if task.Env != nil || len(task.EnvKeys) != 1 {
		t.Fatal("environment leaked")
	}
	r, e := s.RunTask(task.ID, "cli")
	if e != nil {
		t.Fatal(e)
	}
	r = waitRun(t, s, r.ID)
	if r.Status != "failed" || r.ExitCode == nil || *r.ExitCode != 7 {
		t.Fatal(r)
	}
	chunk, e := s.Logs(r.ID, "stderr", 0, 65536)
	if e != nil || string(chunk.Data) != "fail" || !chunk.EOF {
		t.Fatal(chunk, e)
	}
	if _, e = s.SaveTask(task.ID, in, 999); e == nil {
		t.Fatal("accepted stale revision")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if len(s.Tasks()) != 1 || len(s.Runs("")) != 1 {
		t.Fatal("lost data")
	}
	r, e = s.Run(r.ID)
	if e != nil || r.Status != "failed" {
		t.Fatal(r, e)
	}
}
func TestConcurrentTriggers(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	task, e := s.SaveTask("", input("slow", "sleep 120"), 0)
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.RunTask(task.ID, "cli"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	active, skipped := 0, 0
	var id string
	for _, r := range s.Runs("") {
		if r.Active() {
			active++
			id = r.ID
		}
		if r.Status == "skipped" {
			skipped++
		}
	}
	if active != 1 || skipped != 19 {
		t.Fatalf("active=%d skipped=%d", active, skipped)
	}
	if e = s.DeleteTask(task.ID); e == nil {
		t.Fatal("deleted active task")
	}
	s.StopRun(id)
	if r := waitRun(t, s, id); r.Status != "canceled" {
		t.Fatal(r)
	}
}
func TestInterruptedRecovery(t *testing.T) {
	dir := t.TempDir()
	db, e := storage.Open(filepath.Join(dir, "apilot.db"))
	if e != nil {
		t.Fatal(e)
	}
	for _, status := range []string{"queued", "running"} {
		r := model.Run{ID: status, Status: status, CreatedAt: time.Now()}
		if e = db.SaveRun(r); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	for _, r := range s.Runs("") {
		if r.Status != "interrupted" || r.FinishedAt != nil || r.DurationMS != nil || r.FinalizedAt == nil {
			t.Fatal(r)
		}
	}
}
func TestDynamicSchedule(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	in := input("timer", "echo scheduled")
	in.Schedule = &model.Schedule{Type: "interval", Expression: "1s", Enabled: true, Timezone: "UTC"}
	task, e := s.SaveTask("", in, 0)
	if e != nil {
		t.Fatal(e)
	}
	if task.Schedule.NextRunAt == nil {
		t.Fatal("missing next time")
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(s.Runs(task.ID)) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(s.Runs(task.ID)) == 0 {
		t.Fatal("not scheduled")
	}
	s.SetEnabled(task.ID, false)
	before := len(s.Runs(task.ID))
	time.Sleep(1200 * time.Millisecond)
	if len(s.Runs(task.ID)) != before {
		t.Fatal("disabled task ran")
	}
	if _, e = s.RunTask(task.ID, "cli"); e != nil {
		t.Fatal("disabled manual trigger rejected", e)
	}
}
func TestSingleInstance(t *testing.T) {
	dir := t.TempDir()
	s, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if second, e := Open(dir); e == nil {
		second.Close()
		t.Fatal("second owner accepted")
	}
}

func TestResumeSkipsMissedOccurrences(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	in := input("resume", "true")
	in.Schedule = &model.Schedule{Type: "interval", Expression: "1s", Enabled: true, Timezone: "UTC"}
	task, e := s.SaveTask("", in, 0)
	if e != nil {
		t.Fatal(e)
	}
	s.Suspend()
	s.mu.Lock()
	v := s.tasks[task.ID]
	past := time.Now().Add(-10 * time.Minute)
	v.Schedule.NextRunAt = &past
	s.tasks[task.ID] = v
	s.mu.Unlock()
	s.Resume()
	v, e = s.Task(task.ID)
	if e != nil || v.Schedule.NextRunAt == nil || !v.Schedule.NextRunAt.After(time.Now()) {
		t.Fatal(v, e)
	}
	if len(s.Runs("")) != 0 {
		t.Fatal("replayed missed occurrence")
	}
}
