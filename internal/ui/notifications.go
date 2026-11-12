package ui

import (
	"context"
	"log"
	"time"

	"github.com/alswl/apilot/internal/presentation"
	"github.com/egoist/mygo"
)

func (s *windowState) sendNotification(candidate presentation.NotificationCandidate) {
	allowed, err := s.service.AllowNotification(candidate.Key)
	if err != nil || !allowed {
		return
	}
	s.workers.Add(1)
	go func() {
		defer s.workers.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		notification := mygo.NewNotification(mygo.NotificationOptions{Title: presentationMessage(candidate.TitleKey), Body: candidate.Body, Group: "apilot", ID: candidate.Key})
		notification.OnClick(func() {
			s.update(func() {
				s.selectedTask = ""
				s.selectedRun = candidate.RunID
				s.selectedObserver = candidate.ObserverID
				if candidate.ObserverID != "" {
					s.tab = 2
				} else {
					s.tab = 1
				}
				s.show()
			})
		})
		if err := notification.ShowContext(ctx); err != nil {
			log.Printf("notification unavailable: %v", err)
		}
	}()
}
