package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/internal/model"
	"github.com/egoist/mygo"
)

func TestTrayBoundsDynamicContentAndKeepsIdentity(t *testing.T) {
	s := &windowState{err: strings.Repeat("sensitive/path/long-error", 30)}
	for i := 0; i < 40; i++ {
		s.tasks = append(s.tasks, model.Task{ID: fmt.Sprint(i), Name: strings.Repeat("相同的很长任务名称", 10) + fmt.Sprint(i)})
		s.runs = append(s.runs, model.Run{ID: fmt.Sprint(i), TaskID: fmt.Sprint(i), TaskName: s.tasks[i].Name, Status: "running"})
		s.events = append(s.events, model.AttentionEvent{ID: fmt.Sprint(i), Summary: "第一行\n" + strings.Repeat("异常详情", 50)})
	}
	s.runs = append(s.runs, model.Run{ID: "failed", TaskName: "失败任务", Status: "failed"})
	items := s.trayItems(map[string]any{"running_count": 40})
	if len(items) > 20 {
		t.Fatalf("root grows with task count: %d", len(items))
	}
	quick := 0
	var recent *mygo.MenuItem
	for _, item := range items {
		if strings.HasPrefix(item.ID, "task-") {
			quick++
			if item.ToolTip != s.tasks[quick-1].Name || !strings.HasSuffix(item.Label, "…") {
				t.Fatal("truncation lost full task name")
			}
		}
		if item.ID == "recent" {
			recent = item
		}
		for _, child := range item.Submenu {
			if len(child.Submenu) != 0 {
				t.Fatal("third menu level")
			}
			if len([]rune(child.Label)) > 35 || strings.ContainsAny(child.Label, "\n\r\t") {
				t.Fatalf("unbounded label: %q", child.Label)
			}
		}
		if strings.Contains(item.Label, "sensitive/path") {
			t.Fatal("raw error expanded the menu")
		}
	}
	if quick != quickTaskLimit || recent == nil || recent.Submenu[0].ID != "run-failed" {
		t.Fatal("overflow handling or completed-history selection failed")
	}
	if items[1].ID != "attention" || len(items[1].Submenu) != 5 {
		t.Fatal("reminders have no bounded preview and full-list entry")
	}
}

func TestTrayHidesEmptyGroupsButRetainsCoreCommands(t *testing.T) {
	now := time.Now()
	s := &windowState{events: []model.AttentionEvent{{ID: "ack", Summary: "old", AcknowledgedAt: &now}}}
	items := s.trayItems(map[string]any{"running_count": 0})
	found := map[string]bool{}
	for _, item := range items {
		found[item.ID] = true
		if item.ID == "recent" && (len(item.Submenu) != 1 || item.Submenu[0].Label != "全部历史…") {
			t.Fatal("history command disappeared when history is empty")
		}
	}
	if found["attention"] || found["active"] || found["install-cli"] {
		t.Fatal("irrelevant groups or setup command clutter the menu")
	}
	for _, id := range []string{"new-task", "manage", "settings", "recent"} {
		if !found[id] {
			t.Fatalf("missing stable entry %s", id)
		}
	}
}
