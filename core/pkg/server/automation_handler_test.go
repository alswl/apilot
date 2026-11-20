package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func TestAttachedWorkbenchAutomation(t *testing.T) {
	dir := t.TempDir()
	runtime, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	remote := services.AutomationRemote{Remote: services.NewRemote(runtime.SocketPath)}
	initial, err := remote.AutomationSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	settings := initial.Settings
	settings.MaxConcurrent++
	if saved, err := remote.SaveSettings(settings); err != nil || saved.MaxConcurrent != settings.MaxConcurrent {
		t.Fatalf("%+v %v", saved, err)
	}
	task, err := remote.SaveTask("", common.TaskInput{Name: "Native task", Executable: "/bin/echo", Args: []string{"demo log"}, Cwd: dir, TimeoutMS: 3000, Schedule: &common.Schedule{Type: "interval", Expression: "1h", Enabled: false}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := remote.SetAutomatic(task.ID, true, task.Revision)
	if err != nil || !updated.Enabled || !updated.Schedule.Enabled {
		t.Fatalf("%+v %v", updated, err)
	}
	run, err := remote.RunTask(task.ID, "manual")
	if err != nil || run.Trigger != "manual" {
		t.Fatalf("%+v %v", run, err)
	}
	until := time.Now().Add(3 * time.Second)
	finished := false
	for time.Now().Before(until) {
		state, err := remote.AutomationSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range state.Runs {
			if item.ID == run.ID && item.Status == "succeeded" {
				finished = true
			}
		}
		if finished {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	if !finished {
		t.Fatal("run did not finish")
	}
	log, err := remote.Logs(run.ID, "stdout", 0, 65536)
	if err != nil || !bytes.Contains(log.Data, []byte("demo log")) {
		t.Fatalf("%q %v", log.Data, err)
	}
	observer, err := remote.SaveObserver("", common.ObserverInput{Name: "Example", Executable: "/bin/true", Cwd: dir, IntervalMS: 60000, TimeoutMS: 2000, Enabled: true, OutputMode: "text"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	observer, err = remote.SetObserverEnabled(observer.ID, false)
	if err != nil || observer.Enabled {
		t.Fatalf("%+v %v", observer, err)
	}
	if err := remote.DeleteObserver(observer.ID); err != nil {
		t.Fatal(err)
	}
	if err := remote.DeleteTask(task.ID); err != nil {
		t.Fatal(err)
	}
}
