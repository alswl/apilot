package app

import (
	"testing"

	"github.com/alswl/apilot/internal/model"
)

func TestAutomaticPreservesRevisionAndSecrets(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	in := input("auto", "/bin/echo")
	in.EnvSet = map[string]string{"SECRET": "x"}
	in.Schedule = &model.Schedule{Type: "interval", Expression: "1h", Enabled: false}
	task, e := s.SaveTask("", in, 0)
	if e != nil {
		t.Fatal(e)
	}
	updated, e := s.SetAutomatic(task.ID, true, task.Revision)
	if e != nil || !updated.Schedule.Enabled || !updated.Enabled || len(updated.EnvKeys) != 1 {
		t.Fatal(updated, e)
	}
	if _, e = s.SetAutomatic(task.ID, false, task.Revision); e == nil {
		t.Fatal("stale update")
	}
	s.StopAdmission()
	if e = s.DeleteTask(task.ID); e == nil {
		t.Fatal("stopping deletion")
	}
	if _, e = s.SaveSettings(s.Settings()); e == nil {
		t.Fatal("stopping settings")
	}
}
