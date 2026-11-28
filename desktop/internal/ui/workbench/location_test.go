package workbench

import (
	"os"
	"path/filepath"
	"testing"
)

func openLocationTestWorkspace(dir string) *HostedWorkspace {
	m := newFramework()
	m.hosted = true
	m.useAdaptiveLayout()
	m.loadHostedNavigation(dir)
	return &HostedWorkspace{m}
}

func TestWorkbenchLocationRestoresPageAndSavedView(t *testing.T) {
	dir := t.TempDir()
	m := openLocationTestWorkspace(dir).model
	if m.page != tasksPage || m.taskView != "" {
		t.Fatalf("first launch = page %d, view %q", m.page, m.taskView)
	}
	view := savedTaskView{ID: "saved-board", Name: "My board", State: defaultTaskViewState("")}
	view.State.Query, view.State.Board = "priority:high", true
	if err := m.saveNamedTaskView(view); err != nil {
		t.Fatal(err)
	}
	m.selectTaskView(view.ID)
	m.search = "session draft"
	m.openTaskConnections()
	m.navigate(tasksPage)
	if m.taskView != view.ID || m.search != "session draft" || !m.taskBoard {
		t.Fatal("returning to Tasks lost the active browse view")
	}
	m.openTaskConnections()
	if !m.connectionReturnToTasks {
		t.Fatal("connection entry did not establish its transient return path")
	}

	reopened := openLocationTestWorkspace(dir).model
	if reopened.page != connectionsPage || reopened.taskView != view.ID {
		t.Fatalf("relaunch = page %d, view %q", reopened.page, reopened.taskView)
	}
	if reopened.connectionReturnToTasks || reopened.inspector || reopened.fullTask ||
		reopened.taskSelection != "" || reopened.connectorLogin != nil || reopened.connectorEditor != nil {
		t.Fatal("relaunch restored transient state")
	}
	reopened.navigate(tasksPage)
	if reopened.search != view.State.Query || !reopened.taskBoard {
		t.Fatal("relaunch did not apply the saved browse view")
	}
	if err := os.Remove(filepath.Join(dir, "task-views.v2.json")); err != nil {
		t.Fatal(err)
	}
	fallback := openLocationTestWorkspace(dir).model
	if fallback.page != tasksPage || fallback.taskView != "" || fallback.taskBoard || fallback.search != "" {
		t.Fatal("removed saved view did not fall back to the ordinary Tasks view")
	}
}

func TestWorkbenchLocationRestoresCELView(t *testing.T) {
	dir := t.TempDir()
	m := openLocationTestWorkspace(dir).model
	view := m.newTaskQueryView("My query")
	view.Query.Expression = `task.status == "open"`
	if err := m.saveNamedTaskView(view); err != nil {
		t.Fatal(err)
	}
	m.selectTaskView(view.ID)

	reopened := openLocationTestWorkspace(dir).model
	query := reopened.activeTaskQuery()
	if reopened.page != tasksPage || reopened.taskView != view.ID ||
		query == nil || query.Expression != view.Query.Expression {
		t.Fatalf("relaunch = page %d, view %q, query %+v", reopened.page, reopened.taskView, query)
	}
}

func TestWorkbenchLocationFallbackLeavesNavigationUsable(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		directory  bool
	}{
		{name: "malformed", data: `{"page":`},
		{name: "unsupported page", data: `{"page":"settings","task_view":"pending"}`},
		{name: "wrong field type", data: `{"page":"connections","task_view":4}`},
		{name: "unreadable path", directory: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "workbench-location.json")
			var err error
			if tc.directory {
				err = os.Mkdir(path, 0700)
			} else {
				err = os.WriteFile(path, []byte(tc.data), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			m := openLocationTestWorkspace(dir).model
			if m.page != tasksPage || m.taskView != "" {
				t.Fatalf("invalid location = page %d, view %q", m.page, m.taskView)
			}
			m.navigate(connectionsPage)
			if m.page != connectionsPage {
				t.Fatal("persistence failure blocked navigation")
			}
		})
	}
}

func TestWorkbenchLocationExplicitActionWinsAndProfilesAreIsolated(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	m := openLocationTestWorkspace(first).model
	m.selectTaskView("watching")
	m.navigate(connectionsPage)

	other := openLocationTestWorkspace(second).model
	if other.page != tasksPage || other.taskView != "" {
		t.Fatal("new profile inherited another profile's location")
	}
	other.selectTaskView("pending")
	other.navigate(usagePage)

	reopened := openLocationTestWorkspace(first)
	if reopened.model.page != connectionsPage || reopened.model.taskView != "watching" {
		t.Fatal("second profile overwrote the first profile's location")
	}
	reopened.ShowAutomation()
	if reopened.model.page != scheduledPage {
		t.Fatal("explicit automation action before window creation lost to restored state")
	}
	again := openLocationTestWorkspace(first).model
	if again.page != scheduledPage || again.taskView != "watching" {
		t.Fatal("explicit destination was not remembered")
	}
	other = openLocationTestWorkspace(second).model
	if other.page != usagePage || other.taskView != "pending" {
		t.Fatal("first profile overwrote the second profile's location")
	}
}
