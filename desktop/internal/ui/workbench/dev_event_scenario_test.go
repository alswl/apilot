package workbench

import "testing"

func TestDevWorkbenchEvents(t *testing.T) {
	m := newDevEventScenario()
	f := m.devEvents
	if !m.liveLoaded || len(m.objects) != 2 {
		t.Fatal("initial fixture snapshot missing")
	}
	m.receiveLiveDisconnect()
	f.tasks[0].State = "done"
	m.devEvent("changed")
	if !m.liveStale || m.objects[0].State != "open" {
		t.Fatal("disconnected fixture replaced the stale snapshot")
	}
	m.devEvent("snapshot")
	if m.liveStale || m.objects[0].State != "done" {
		t.Fatal("reconnection did not reread fixture")
	}
	m.newTaskSourceID = "dev-github"
	m.newTaskOpen, m.newTaskDraft = true, "protected"
	f.snapshots[1].AccessState = "unavailable"
	m.devEvent("changed")
	if len(m.objects) != 1 || m.newTaskOpen || m.newTaskDraft != "" {
		t.Fatal("authorization change retained protected state")
	}
	f.fail = true
	m.devEvent("changed")
	if !m.liveStale || len(m.objects) != 1 {
		t.Fatal("temporary read failure lost last confirmed state")
	}
}
