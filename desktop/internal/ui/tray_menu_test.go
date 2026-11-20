package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/presentation"
	"github.com/egoist/mygo"
)

func TestTrayBoundsDynamicContentAndKeepsIdentity(t *testing.T) {
	s := &windowState{err: strings.Repeat("sensitive/path/long-error", 30)}
	for i := 0; i < 40; i++ {
		s.tasks = append(s.tasks, common.Task{ID: fmt.Sprint(i), Name: strings.Repeat("相同的很长任务名称", 10) + fmt.Sprint(i)})
		s.runs = append(s.runs, common.Run{ID: fmt.Sprint(i), TaskID: fmt.Sprint(i), TaskName: s.tasks[i].Name, Status: "running"})
		s.events = append(s.events, common.AttentionEvent{ID: fmt.Sprint(i), Summary: "第一行\n" + strings.Repeat("异常详情", 50)})
	}
	s.runs = append(s.runs, common.Run{ID: "failed", TaskName: "失败任务", Status: "failed"})
	items := s.trayItems(map[string]any{"running_count": 40})
	if len(items) > 20 {
		t.Fatalf("root grows with task count: %d", len(items))
	}
	if items[0].ID != "workbench" {
		t.Fatal("the first command must open the main window")
	}
	groups := map[string]*mygo.MenuItem{}
	for _, item := range items {
		groups[item.ID] = item
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
	quick := groups["quick-tasks"]
	if quick == nil || len(quick.Submenu) != presentation.QuickTaskLimit || groups["recent"] != nil {
		t.Fatal("expected bounded task shortcuts and history in the workbench")
	}
	for i, item := range quick.Submenu {
		if item.ID != "task-"+s.tasks[i].ID || item.ToolTip != s.tasks[i].Name || !strings.HasSuffix(item.Label, "…") {
			t.Fatal("truncation lost task identity")
		}
	}
	if attention := groups["attention"]; attention == nil || len(attention.Submenu) != presentation.PreviewLimit+2 {
		t.Fatal("reminders have no bounded preview and full-list entry")
	}
}

func TestTrayHidesEmptyGroupsButRetainsCoreCommands(t *testing.T) {
	now := time.Now()
	s := &windowState{events: []common.AttentionEvent{{ID: "ack", Summary: "old", AcknowledgedAt: &now}}}
	items := s.trayItems(map[string]any{"running_count": 0})
	found := map[string]bool{}
	for _, item := range items {
		found[item.ID] = true
	}
	if found["attention"] || found["active"] || found["quick-tasks"] || found["recent"] || found["install-cli"] {
		t.Fatal("irrelevant groups or setup command clutter the menu")
	}
	for _, id := range []string{"workbench", "new-task", "manage", "settings", "quit"} {
		if !found[id] {
			t.Fatalf("missing stable entry %s", id)
		}
	}
}
