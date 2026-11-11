package app

import (
	"github.com/alswl/apilot/internal/model"
	"testing"
)

func TestRunTransitions(t *testing.T) {
	states := []string{"queued", "running", "succeeded", "failed", "canceled", "interrupted", "skipped"}
	for _, from := range states {
		for _, to := range states {
			allowed := false
			if from == "queued" {
				allowed = to == "running" || to == "failed" || to == "canceled" || to == "interrupted" || to == "skipped"
			}
			if from == "running" {
				allowed = to == "succeeded" || to == "failed" || to == "canceled" || to == "interrupted"
			}
			if model.CanTransition(from, to) != allowed {
				t.Fatal(from, to)
			}
		}
	}
}
func TestStartupFailureHasNoExecutionTimes(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	in := input("missing", "")
	in.Executable = "/nonexistent/apilot-command"
	task, e := s.SaveTask("", in, 0)
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.RunTask(task.ID, "cli")
	if e != nil {
		t.Fatal(e)
	}
	r = waitRun(t, s, r.ID)
	if r.Status != "failed" || r.StartedAt != nil || r.FinishedAt != nil || r.ExitCode != nil {
		t.Fatal(r)
	}
}
