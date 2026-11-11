package ui

import (
	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/model"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

func TestCompactTaskNavigation(t *testing.T) {
	for _, size := range [][2]int{{640, 480}, {480, 360}} {
		s := &windowState{tasks: []model.Task{{ID: "demo", Name: "长任务名称 demo", Enabled: true, Schedule: &model.Schedule{Type: "cron", Expression: "0 9 * * 1-5", Enabled: true}}}}
		tt := ui.NewTester(s.view, size[0], size[1])
		if !tt.HasText("任务") || !tt.HasText("历史") || !tt.HasText("观察") {
			t.Fatal(tt.Texts())
		}
		if e := tt.Click("长任务名称 demo"); e != nil {
			t.Fatal(e)
		}
		if s.selectedTask != "demo" || !tt.HasText("自动运行") {
			t.Fatal(s.selectedTask, tt.Texts())
		}
		if e := tt.Click("返回任务"); e != nil {
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
	if !tt.HasText("暂无执行记录") {
		t.Fatal(tt.Texts())
	}
	s.runs = []model.Run{{ID: "run", TaskName: "demo", Status: "failed", CreatedAt: time.Now()}}
	tt = ui.NewTester(s.view, 640, 480)
	if e := tt.Click("demo 失败"); e != nil {
		t.Fatal(e)
	}
	if s.selectedRun != "run" || !tt.HasText("标准输出") {
		t.Fatal(tt.Texts())
	}
}

func TestAttentionRoutesToSource(t *testing.T) {
	s := &windowState{selectedTask: "previous"}
	s.selectEvent(model.AttentionEvent{SourceType: "observer", SourceID: "observer"})
	if s.tab != 2 || s.selectedObserver != "observer" || s.selectedRun != "" || s.selectedTask != "" {
		t.Fatal("observer event did not open observation")
	}
	s.selectEvent(model.AttentionEvent{SourceType: "task", RunID: "run"})
	if s.tab != 1 || s.selectedRun != "run" || s.selectedObserver != "" {
		t.Fatal("task event did not open run")
	}
}

func TestMenuOffersDailyActionsWithoutMainWindow(t *testing.T) {
	s := &windowState{tasks: []model.Task{{ID: "task", Name: "review", Enabled: true, Schedule: &model.Schedule{Enabled: true}}}, runs: []model.Run{{ID: "run", TaskID: "task", TaskName: "review", Status: "running"}}}
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
	for _, label := range []string{"停止「review」", "立即运行", "自动运行", "新建任务…", "全部任务…", "设置…"} {
		if found[label] == nil {
			t.Fatalf("missing menu action %q", label)
		}
	}
	if !found["立即运行"].Disabled {
		t.Fatal("active task can be triggered again")
	}
	if !found["自动运行"].Checked {
		t.Fatal("schedule state not reflected")
	}
	if s.win != nil {
		t.Fatal("menu needs main window")
	}
}

func TestAutomaticToggleEnablesDisabledPlanAndPreservesSecrets(t *testing.T) {
	service, err := app.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	task, err := service.SaveTask("", model.TaskInput{Name: "review", Executable: "/bin/echo", Cwd: t.TempDir(), Enabled: false, TimeoutMS: 1000, EnvSet: map[string]string{"TOKEN": "private"}, Schedule: &model.Schedule{Type: "interval", Expression: "1h", Enabled: false}}, 0)
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
