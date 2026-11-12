package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/spf13/cobra"
)

func (c *client) saveObserveInput(op string, cmd *cobra.Command, a []string) error {
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	in := common.ObserverInput{Name: a[0], Cwd: cwd, IntervalMS: 30000, TimeoutMS: 10000, Enabled: true, OutputMode: "text"}
	expected := int64(0)
	if op == "update" {
		b, e := c.call("observer.get", map[string]any{"observer": a[0]})
		if e != nil {
			return e
		}
		var v common.Observer
		if e = json.Unmarshal(b, &v); e != nil {
			return e
		}
		in = common.ObserverInput{Name: v.Name, Executable: v.Executable, Args: v.Args, Cwd: v.Cwd, IntervalMS: v.IntervalMS, TimeoutMS: v.TimeoutMS, Enabled: v.Enabled, OutputMode: v.OutputMode}
		expected = v.Revision
	}
	f := cmd.Flags()
	for name, dst := range map[string]*string{"cwd": &in.Cwd, "output": &in.OutputMode, "name": &in.Name} {
		if f.Changed(name) {
			*dst, _ = f.GetString(name)
		}
	}
	if f.Changed("interval") {
		d, _ := f.GetDuration("interval")
		in.IntervalMS = d.Milliseconds()
	}
	if f.Changed("timeout") {
		d, _ := f.GetDuration("timeout")
		in.TimeoutMS = d.Milliseconds()
	}
	if f.Changed("disabled") {
		v, _ := f.GetBool("disabled")
		in.Enabled = !v
	}
	if cmd.ArgsLenAtDash() >= 0 {
		if cmd.ArgsLenAtDash() != 1 || len(a) < 2 {
			return common.Err("INVALID_ARGUMENT", "Use OBSERVER -- EXECUTABLE [ARGS...]")
		}
		in.Executable = a[1]
		in.Args = a[2:]
	} else if len(a) > 1 {
		return common.Err("INVALID_ARGUMENT", "Separate command with --")
	}
	if f.Changed("env-file") {
		path, _ := f.GetString("env-file")
		in.EnvSet, e = readEnv(path)
		if e != nil {
			return e
		}
	}
	in.EnvUnset, _ = f.GetStringArray("env-unset")
	if f.Changed("if-revision") {
		expected, _ = f.GetInt64("if-revision")
	}
	if op == "add" {
		return c.invoke("observer.create", in)
	}
	return c.invoke("observer.update", map[string]any{"observer": a[0], "patch": in, "expected_revision": expected})
}
func configureObserveInputFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("name", "", "Observer name")
	f.Duration("interval", 30*time.Second, "Polling interval")
	f.Duration("timeout", 10*time.Second, "Polling timeout")
	f.String("cwd", "", "Working directory")
	f.String("output", "text", "text or json")
	f.Bool("disabled", false, "Disable polling")
	f.String("env-file", "", "Read KEY=VALUE environment file")
	f.StringArray("env-unset", nil, "Remove environment key")
	f.Int64("if-revision", 0, "Require revision")
}
