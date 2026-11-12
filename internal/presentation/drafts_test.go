package presentation

import (
	"testing"

	"github.com/alswl/apilot/internal/model"
)

func TestTaskDraftRoundTrip(t *testing.T) {
	original := model.Task{ID: "t", Revision: 7, Name: "task", Executable: "/bin/echo", Args: []string{"a", "", "b"}, Cwd: "/tmp", Enabled: true, TimeoutMS: 1000, Schedule: &model.Schedule{ID: "s", Type: "interval", Expression: "1h", Enabled: false}}
	d := NewTaskDraft(&original, model.DefaultSettings(), "/home")
	d.Env = "TOKEN=a=b\n\nTOKEN=c=d"
	d.Unset = "OLD\n"
	in, e := d.Input()
	if e != nil {
		t.Fatal(e)
	}
	if in.Args[1] != "" || in.EnvSet["TOKEN"] != "c=d" || len(in.EnvUnset) != 1 || in.Schedule.ID != "s" || in.Schedule.Enabled {
		t.Fatalf("%+v", in)
	}
	in.Schedule.Enabled = true
	if original.Schedule.Enabled {
		t.Fatal("draft aliases original")
	}
	d.Mode = 1
	d.Command = "echo hi"
	in, e = d.Input()
	if e != nil || in.Executable != "/bin/zsh" || in.Args[1] != "echo hi" {
		t.Fatal(in, e)
	}
	for _, v := range []string{"0s", "-1s", "1ns", "999999999999h"} {
		d.Timeout = v
		if _, e = d.Input(); e == nil {
			t.Fatal(v)
		}
	}
	d.Timeout = "1s"
	d.Env = "bad"
	if _, e = d.Input(); e == nil {
		t.Fatal("invalid env accepted")
	}
}
func TestObserverAndSettingsConversion(t *testing.T) {
	d := NewObserverDraft(nil, "/tmp")
	d.Values.Name = "observer"
	d.Values.Executable = "/bin/echo"
	d.Args = "a\n\nb"
	in, e := d.Input()
	if e != nil || len(in.Args) != 3 || in.IntervalMS != 30000 {
		t.Fatal(in, e)
	}
	d.Timeout = "1ns"
	if _, e = d.Input(); e == nil {
		t.Fatal("sub-millisecond timeout")
	}
	s := NewSettingsDraft(model.DefaultSettings())
	s.Limit = "9223372036854775807"
	if _, e = s.Settings(); e == nil {
		t.Fatal("MiB overflow")
	}
	s = NewSettingsDraft(model.DefaultSettings())
	v, e := s.Settings()
	if e != nil || v != model.DefaultSettings() {
		t.Fatal(v, e)
	}
}
