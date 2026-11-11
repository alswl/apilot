package cli

import (
	"encoding/json"
	"github.com/alswl/apilot/internal/model"
	"github.com/spf13/cobra"
	"os"
	"time"
)

func (c *client) observeCommands(root *cobra.Command) {
	g := &cobra.Command{Use: "observe", Short: "Read external tool state by polling explicit commands"}
	root.AddCommand(g)
	g.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return c.invoke("observer.list", nil) }})
	for _, op := range []string{"show", "remove", "enable", "disable"} {
		op := op
		g.AddCommand(&cobra.Command{Use: op + " OBSERVER", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			method := map[string]string{"show": "get", "remove": "remove", "enable": "set_enabled", "disable": "set_enabled"}[op]
			return c.invoke("observer."+method, map[string]any{"observer": a[0], "enabled": op == "enable"})
		}})
	}
	for _, op := range []string{"add", "update"} {
		op := op
		cmd := &cobra.Command{Use: op + " OBSERVER -- EXECUTABLE [ARGS...]", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			cwd, e := os.Getwd()
			if e != nil {
				return e
			}
			in := model.ObserverInput{Name: a[0], Cwd: cwd, IntervalMS: 30000, TimeoutMS: 10000, Enabled: true, OutputMode: "text"}
			expected := int64(0)
			if op == "update" {
				b, e := c.call("observer.get", map[string]any{"observer": a[0]})
				if e != nil {
					return e
				}
				var v model.Observer
				if e = json.Unmarshal(b, &v); e != nil {
					return e
				}
				in = model.ObserverInput{Name: v.Name, Executable: v.Executable, Args: v.Args, Cwd: v.Cwd, IntervalMS: v.IntervalMS, TimeoutMS: v.TimeoutMS, Enabled: v.Enabled, OutputMode: v.OutputMode}
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
					return model.Err("INVALID_ARGUMENT", "Use OBSERVER -- EXECUTABLE [ARGS...]")
				}
				in.Executable = a[1]
				in.Args = a[2:]
			} else if len(a) > 1 {
				return model.Err("INVALID_ARGUMENT", "Separate command with --")
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
		}}
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
		g.AddCommand(cmd)
	}
}
