package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/server"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestCompactTaskNavigation(t *testing.T) {
	for _, size := range [][2]int{{640, 480}, {480, 360}} {
		s := &windowState{tasks: []common.Task{{ID: "demo", Name: "长任务名称 demo", Enabled: true, Schedule: &common.Schedule{Type: "cron", Expression: "0 9 * * 1-5", Enabled: true}}}}
		tt := ui.NewTester(s.view, size[0], size[1])
		if !tt.HasText(tr("tasks")) || !tt.HasText(tr("history")) || !tt.HasText(tr("observers")) {
			t.Fatal(tt.Texts())
		}
		if e := tt.Click("长任务名称 demo"); e != nil {
			t.Fatal(e)
		}
		if s.selectedTask != "demo" || !tt.HasText(tr("automaticRuns")) {
			t.Fatal(s.selectedTask, tt.Texts())
		}
		if e := tt.Click(tr("backToTasks")); e != nil {
			t.Fatal(e)
		}
		if s.selectedTask != "" {
			t.Fatal("did not return")
		}
	}
}
func TestEmptyHistoryAndResultLabels(t *testing.T) {
	s := &windowState{tab: 1}
	tt := ui.NewTester(s.view, 640, 480)
	if !tt.HasText(tr("noRunsYet")) {
		t.Fatal(tt.Texts())
	}
	s.runs = []common.Run{{ID: "run", TaskName: "demo", Status: "failed", CreatedAt: time.Now()}}
	tt = ui.NewTester(s.view, 640, 480)
	if e := tt.Click("demo " + tr("failed")); e != nil {
		t.Fatal(e)
	}
	if s.selectedRun != "run" || !tt.HasText(tr("standardOutput")) {
		t.Fatal(tt.Texts())
	}
}

func TestAttentionRoutesToSource(t *testing.T) {
	s := &windowState{selectedTask: "previous"}
	s.selectEvent(common.AttentionEvent{SourceType: "observer", SourceID: "observer"})
	if s.tab != 2 || s.selectedObserver != "observer" || s.selectedRun != "" || s.selectedTask != "" {
		t.Fatal("observer event did not open observation")
	}
	s.selectEvent(common.AttentionEvent{SourceType: "task", RunID: "run"})
	if s.tab != 1 || s.selectedRun != "run" || s.selectedObserver != "" {
		t.Fatal("task event did not open run")
	}
}

func TestMenuOffersDailyActionsWithoutMainWindow(t *testing.T) {
	s := &windowState{tasks: []common.Task{{ID: "task", Name: "review", Enabled: true, Schedule: &common.Schedule{Enabled: true}}}, runs: []common.Run{{ID: "run", TaskID: "task", TaskName: "review", Status: "running"}}}
	items := s.trayItems(map[string]any{"running_count": 1, "attention_count": 0})
	found := map[string]*mygo.MenuItem{}
	var walk func([]*mygo.MenuItem)
	walk = func(items []*mygo.MenuItem) {
		for _, item := range items {
			found[item.Label] = item
			walk(item.Submenu)
		}
	}
	walk(items)
	for _, label := range []string{fmt.Sprintf(tr("stopS"), "review"), tr("runNow"), tr("automaticRuns"), tr("newTask2"), tr("allTasks"), tr("settings2")} {
		if found[label] == nil {
			t.Fatalf("missing menu action %q", label)
		}
	}
	if !found[tr("runNow")].Disabled {
		t.Fatal("active task can be triggered again")
	}
	if !found[tr("automaticRuns")].Checked {
		t.Fatal("schedule state not reflected")
	}
	if s.win != nil {
		t.Fatal("menu needs main window")
	}
}

func TestAutomaticToggleEnablesDisabledPlanAndPreservesSecrets(t *testing.T) {
	service, err := server.OpenService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	task, err := service.SaveTask("", common.TaskInput{Name: "review", Executable: "/bin/echo", Cwd: t.TempDir(), Enabled: false, TimeoutMS: 1000, EnvSet: map[string]string{"TOKEN": "private"}, Schedule: &common.Schedule{Type: "interval", Expression: "1h", Enabled: false}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	s := &windowState{service: service}
	if err := s.setAutomatic(task); err != nil {
		t.Fatal(err)
	}
	updated := service.Tasks()[0]
	if !updated.Enabled || !updated.Schedule.Enabled || updated.Schedule.NextRunAt == nil {
		t.Fatal("schedule did not activate")
	}
	if len(updated.EnvKeys) != 1 || updated.EnvKeys[0] != "TOKEN" {
		t.Fatal("environment changed")
	}
	if err := s.setAutomatic(task); err == nil {
		t.Fatal("stale menu overwrote updated task")
	}
	if err := s.setAutomatic(updated); err != nil {
		t.Fatal(err)
	}
	if service.Tasks()[0].Enabled {
		t.Fatal("toggle off failed")
	}
}
