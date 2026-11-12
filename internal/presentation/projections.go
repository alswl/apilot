package presentation

import (
	"strings"
	"unicode"

	"github.com/alswl/apilot/internal/model"
	"golang.org/x/text/width"
)

const QuickTaskLimit = 5
const PreviewLimit = 3

func MenuLabel(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	var out strings.Builder
	units := 0
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		n := 1
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n = 2
		}
		if unicode.Is(unicode.Mn, r) {
			n = 0
		}
		if units+n > 28 {
			out.WriteRune('…')
			break
		}
		out.WriteRune(r)
		units += n
	}
	return out.String()
}
func TaskActive(runs []model.Run, id string) bool {
	for _, r := range runs {
		if r.TaskID == id && r.Active() {
			return true
		}
	}
	return false
}

type Target struct {
	Tab               int
	RunID, ObserverID string
}

func EventTarget(e model.AttentionEvent) Target {
	if e.SourceType == "observer" {
		return Target{Tab: 2, ObserverID: e.SourceID}
	}
	return Target{Tab: 1, RunID: e.RunID}
}

type MenuProjection struct {
	Tasks                    []model.Task
	Active, Recent           []model.Run
	Attention                []model.AttentionEvent
	ActiveCount, UnreadCount int
}

func ProjectMenu(tasks []model.Task, runs []model.Run, events []model.AttentionEvent) MenuProjection {
	p := MenuProjection{Tasks: tasks}
	if len(p.Tasks) > QuickTaskLimit {
		p.Tasks = p.Tasks[:QuickTaskLimit]
	}
	for _, r := range runs {
		if r.Active() {
			p.ActiveCount++
			if len(p.Active) < PreviewLimit {
				p.Active = append(p.Active, r)
			}
		} else if len(p.Recent) < PreviewLimit {
			p.Recent = append(p.Recent, r)
		}
	}
	for _, e := range events {
		if e.AcknowledgedAt == nil {
			p.UnreadCount++
			if len(p.Attention) < PreviewLimit {
				p.Attention = append(p.Attention, e)
			}
		}
	}
	return p
}

type NotificationCandidate struct{ Key, TitleKey, Body, RunID, ObserverID string }

func InitialSeen(runs []model.Run, events []model.AttentionEvent) map[string]bool {
	seen := map[string]bool{}
	for _, r := range runs {
		if !r.Active() {
			seen[r.ID] = true
		}
	}
	for _, e := range events {
		seen[e.ID] = true
	}
	return seen
}
func Notifications(settings model.Settings, runs []model.Run, events []model.AttentionEvent, seen map[string]bool) []NotificationCandidate {
	var out []NotificationCandidate
	for _, e := range events {
		if seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		if e.SourceType == "task" && settings.NotifyFailure {
			out = append(out, NotificationCandidate{e.SourceID + ":failed", "taskFailed", e.Summary, e.RunID, ""})
		}
		if e.SourceType == "observer" && settings.NotifyObserver {
			out = append(out, NotificationCandidate{e.SourceID + ":changed", "observerStateChanged", e.Summary, "", e.SourceID})
		}
	}
	for _, r := range runs {
		if r.Active() || seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if r.Status == "succeeded" && settings.NotifySuccess {
			out = append(out, NotificationCandidate{r.TaskID + ":success", "taskCompleted", r.TaskName, r.ID, ""})
		}
	}
	return out
}
