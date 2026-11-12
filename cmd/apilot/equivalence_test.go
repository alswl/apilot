package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/alswl/apilot/internal/presentation"
	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/config"
	"github.com/alswl/apilot/pkg/server"
)

func TestPresentationAndCLIShareBusinessRules(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	s, e := server.OpenService(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	endpoint, e := server.Listen(s, config.SocketPath(dir))
	if e != nil {
		t.Fatal(e)
	}
	defer endpoint.Close()
	call := func(args ...string) map[string]json.RawMessage {
		t.Helper()
		var out, err bytes.Buffer
		if exit := execute(args, &out, &err); exit != 0 {
			t.Fatalf("%v: %d %s %s", args, exit, out.String(), err.String())
		}
		var response map[string]json.RawMessage
		if e := json.Unmarshal(out.Bytes(), &response); e != nil {
			t.Fatal(e)
		}
		return response
	}
	d := presentation.NewTaskDraft(nil, common.DefaultSettings(), dir)
	d.Values.Name = "gui"
	d.Command = "/bin/echo"
	d.Args = "hello"
	d.Plan = 2
	d.Expression = "1h"
	d.Env = "TOKEN=a=b"
	in, e2 := d.Input()
	if e2 != nil {
		t.Fatal(e2)
	}
	gui, e := s.SaveTask("", in, 0)
	if e != nil {
		t.Fatal(e)
	}
	call("--json", "task", "add", "cli", "--interval", "1h", "--cwd", dir, "--", "/bin/echo", "hello")
	cliTask, e := s.Task("cli")
	if e != nil {
		t.Fatal(e)
	}
	if gui.Executable != cliTask.Executable || gui.Schedule.Expression != cliTask.Schedule.Expression || gui.TimeoutMS != cliTask.TimeoutMS {
		t.Fatal(gui, cliTask)
	}
	var run common.Run
	json.Unmarshal(call("--json", "task", "run", "cli")["data"], &run)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, e = s.Run(run.ID)
		if e != nil {
			t.Fatal(e)
		}
		if !run.Active() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.Status != "succeeded" {
		t.Fatal(run)
	}
	call("--json", "run", "show", run.ID)
	od := presentation.NewObserverDraft(nil, dir)
	od.Values.Name = "observer"
	od.Values.Executable = "/bin/echo"
	oin, oe := od.Input()
	if oe != nil {
		t.Fatal(oe)
	}
	if _, e = s.SaveObserver("", oin, 0); e != nil {
		t.Fatal(e)
	}
	call("--json", "observe", "list")
	call("--json", "event", "list")
	call("--json", "settings", "show")
	settings := presentation.NewSettingsDraft(s.Settings())
	settings.Max = "3"
	v, fe := settings.Settings()
	if fe != nil {
		t.Fatal(fe)
	}
	if _, e = s.SaveSettings(v); e != nil {
		t.Fatal(e)
	}
	call("--json", "settings", "show")
}

func TestCommandRegistrationResetsParseState(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	runtime, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	var out, errout bytes.Buffer
	for _, args := range [][]string{
		{"--json", "task", "add", "repeat", "--", "/bin/echo", "first"},
		{"--json", "task", "update", "repeat", "--", "/bin/echo", "second"},
		{"--json", "task", "update", "repeat", "--name", "renamed"},
		{"help"},
	} {
		out.Reset()
		errout.Reset()
		if code := execute(args, &out, &errout); code != 0 {
			t.Fatalf("%v: %d %s %s", args, code, out.String(), errout.String())
		}
	}
}
