package presentation

import (
	"testing"

	"github.com/alswl/apilot/pkg/common"
)

func TestMenuAndNotificationProjection(t *testing.T) {
	runs := []common.Run{{ID: "old", Status: "succeeded", TaskID: "t"}, {ID: "active", Status: "running", TaskID: "t"}}
	seen := InitialSeen(runs, nil)
	if !seen["old"] || seen["active"] {
		t.Fatal(seen)
	}
	runs = append(runs, common.Run{ID: "new", Status: "succeeded", TaskID: "t"})
	settings := common.DefaultSettings()
	settings.NotifySuccess = true
	c := Notifications(settings, runs, nil, seen)
	if len(c) != 1 || c[0].RunID != "new" {
		t.Fatal(c)
	}
	if len(Notifications(settings, runs, nil, seen)) != 0 {
		t.Fatal("duplicate")
	}
	p := ProjectMenu(make([]common.Task, 10), runs, nil)
	if len(p.Tasks) != 5 || p.ActiveCount != 1 || !TaskActive(runs, "t") {
		t.Fatal(p)
	}
	if EventTarget(common.AttentionEvent{SourceType: "observer", SourceID: "o"}).ObserverID != "o" {
		t.Fatal("route")
	}
}

func TestMenuProjectionKeepsTotalsAndBounds(t *testing.T) {
	var runs []common.Run
	var events []common.AttentionEvent
	for i := 0; i < 8; i++ {
		runs = append(runs, common.Run{Status: "running"}, common.Run{Status: "failed"})
		events = append(events, common.AttentionEvent{})
	}
	p := ProjectMenu(nil, runs, events)
	if p.ActiveCount != 8 || p.UnreadCount != 8 || len(p.Active) != PreviewLimit || len(p.Recent) != PreviewLimit || len(p.Attention) != PreviewLimit {
		t.Fatal(p)
	}
}
