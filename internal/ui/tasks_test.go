package ui

import (
	"github.com/alswl/apilot/internal/model"
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
