package ui

import (
	"context"
	"github.com/egoist/mygo"
	"log"
	"time"
)

func (s *windowState) notify(ctx context.Context, seen map[string]bool) {
	settings := s.service.Settings()
	events, e := s.service.Events()
	if e != nil {
		return
	}
	send := func(key, title, body, runID, observerID string) {
		allowed, e := s.service.AllowNotification(key)
		if e != nil || !allowed {
			return
		}
		go func() {
			notificationCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			notification := mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body, Group: "apilot", ID: key})
			notification.OnClick(func() {
				s.update(func() {
					s.selectedTask = ""
					s.selectedRun = runID
					s.selectedObserver = observerID
					if observerID != "" {
						s.tab = 2
					} else {
						s.tab = 1
					}
					s.show()
				})
			})
			if e := notification.ShowContext(notificationCtx); e != nil {
				log.Printf("notification unavailable: %v", e)
			}
		}()
	}
	for _, v := range events {
		if seen[v.ID] {
			continue
		}
		seen[v.ID] = true
		if v.SourceType == "task" && settings.NotifyFailure {
			send(v.SourceID+":failed", tr("taskFailed"), v.Summary, v.RunID, "")
		}
		if v.SourceType == "observer" && settings.NotifyObserver {
			send(v.SourceID+":changed", tr("observerStateChanged"), v.Summary, "", v.SourceID)
		}
	}
	for _, r := range s.service.Runs("") {
		if r.Active() || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if r.Status == "succeeded" && settings.NotifySuccess {
			send(r.TaskID+":success", tr("taskCompleted"), r.TaskName, r.ID, "")
		}
	}
}
