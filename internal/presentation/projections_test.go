package presentation

import (
	"testing"

	"github.com/alswl/apilot/internal/model"
)

func TestMenuAndNotificationProjection(t *testing.T) {
	runs := []model.Run{{ID: "old", Status: "succeeded", TaskID: "t"}, {ID: "active", Status: "running", TaskID: "t"}}
	seen := InitialSeen(runs, nil)
	if !seen["old"] || seen["active"] {
		t.Fatal(seen)
	}
	runs = append(runs, model.Run{ID: "new", Status: "succeeded", TaskID: "t"})
	settings := model.DefaultSettings()
	settings.NotifySuccess = true
	c := Notifications(settings, runs, nil, seen)
	if len(c) != 1 || c[0].RunID != "new" {
		t.Fatal(c)
	}
	if len(Notifications(settings, runs, nil, seen)) != 0 {
		t.Fatal("duplicate")
	}
	p := ProjectMenu(make([]model.Task, 10), runs, nil)
	if len(p.Tasks) != 5 || p.ActiveCount != 1 || !TaskActive(runs, "t") {
		t.Fatal(p)
	}
	if EventTarget(model.AttentionEvent{SourceType: "observer", SourceID: "o"}).ObserverID != "o" {
		t.Fatal("route")
	}
}

func TestMenuProjectionKeepsTotalsAndBounds(t *testing.T) {
	var runs []model.Run
	var events []model.AttentionEvent
	for i := 0; i < 8; i++ {
		runs = append(runs, model.Run{Status: "running"}, model.Run{Status: "failed"})
		events = append(events, model.AttentionEvent{})
	}
	p := ProjectMenu(nil, runs, events)
	if p.ActiveCount != 8 || p.UnreadCount != 8 || len(p.Active) != PreviewLimit || len(p.Recent) != PreviewLimit || len(p.Attention) != PreviewLimit {
		t.Fatal(p)
	}
}
