package cli

import (
	"encoding/json"
	"fmt"
	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/ipc"
	"github.com/alswl/apilot/internal/model"
	"github.com/alswl/apilot/internal/storage"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestTargetDatasetQueries(t *testing.T) {
	dir := t.TempDir()
	db, e := storage.Open(filepath.Join(dir, "apilot.db"))
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	for i := 0; i < 100; i++ {
		task := model.Task{ID: fmt.Sprintf("task-%d", i), Name: fmt.Sprintf("task-%03d", i), Executable: "/bin/true", Args: []string{}, Cwd: "/tmp", Enabled: true, TimeoutMS: 1000, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if e = db.SaveTask(task); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 1000; i++ {
		r := model.Run{ID: fmt.Sprintf("run-%04d", i), TaskID: fmt.Sprintf("task-%d", i%100), Status: "succeeded", CreatedAt: now}
		if e = db.SaveRun(r); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	s, e := app.Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	server, e := ipc.Listen(s, app.SocketPath(dir))
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	durations := []time.Duration{}
	for i := 0; i < 30; i++ {
		start := time.Now()
		b, e := ipc.Call(app.SocketPath(dir), "task.list", map[string]any{"limit": 100})
		if e != nil || !json.Valid(b) {
			t.Fatal(e)
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[28]
	t.Logf("100 tasks/1000 runs: list p95=%s", p95)
	if p95 > time.Second {
		t.Fatal("query exceeded target")
	}
}
