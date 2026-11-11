package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/ipc"
	"github.com/alswl/apilot/internal/model"
	"github.com/spf13/cobra"
	"io"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"
)

type client struct {
	path string
	json bool
	out  io.Writer
}

func (c *client) call(method string, p any) (json.RawMessage, error) {
	return ipc.Call(c.path, method, p)
}
func (c *client) show(data any) error {
	if c.json {
		return json.NewEncoder(c.out).Encode(ipc.Response{Version: 1, Data: data})
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) == nil {
		if items, ok := object["items"].([]any); ok {
			w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATE\tID")
			for _, item := range items {
				row, ok := item.(map[string]any)
				if !ok {
					continue
				}
				name, _ := row["name"].(string)
				if name == "" {
					name, _ = row["task_name"].(string)
				}
				if name == "" {
					name, _ = row["summary"].(string)
				}
				state, _ := row["status"].(string)
				if state == "" {
					state, _ = row["health"].(string)
				}
				if state == "" {
					if run, ok := row["last_run"].(map[string]any); ok {
						state, _ = run["status"].(string)
					}
				}
				if enabled, ok := row["enabled"].(bool); ok && !enabled {
					state += " (auto off)"
				}
				fmt.Fprintf(w, "%s\t%s\t%v\n", name, state, row["id"])
			}
			return w.Flush()
		}
		if state, ok := object["state"].(string); ok {
			_, e := fmt.Fprintf(c.out, "apilot %s\nRunning tasks: %v\nNeeds attention: %v\n", state, object["running_count"], object["attention_count"])
			return e
		}
		if v, ok := object["app_version"].(string); ok && len(object) == 2 {
			_, e := fmt.Fprintln(c.out, "apilot "+v)
			return e
		}
	}
	b, e := json.MarshalIndent(data, "", "  ")
	if e == nil {
		_, e = fmt.Fprintln(c.out, string(b))
	}
	return e
}
func (c *client) invoke(method string, p any) error {
	b, e := c.call(method, p)
	if e != nil {
		return e
	}
	return c.show(b)
}
func Execute(args []string, out, errout io.Writer) int {
	c := &client{path: app.SocketPath(app.DataDir()), out: out}
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--json" {
			c.json = true
		}
	}
	root := &cobra.Command{Use: "apilot", Short: "Manage local tasks in the running apilot App", SilenceErrors: true, SilenceUsage: true}
	root.SetOut(out)
	root.SetErr(errout)
	root.SetArgs(args)
	root.PersistentFlags().BoolVar(&c.json, "json", c.json, "Output versioned JSON")
	root.AddCommand(&cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		return c.show(map[string]any{"app_version": "0.1.0-dev", "protocol_version": 1})
	}})
	root.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return c.invoke("status", nil) }})
	root.AddCommand(&cobra.Command{Use: "doctor", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		data, e := c.call("doctor", nil)
		if e == nil {
			return c.show(data)
		}
		paths := map[string]any{}
		for _, p := range []string{app.DataDir(), app.DataDir() + "/logs"} {
			st, err := os.Stat(p)
			paths[p] = map[string]any{"exists": err == nil, "directory": err == nil && st.IsDir()}
		}
		if c.json {
			_ = json.NewEncoder(out).Encode(ipc.Response{Version: 1, Data: map[string]any{"paths": paths}, Error: ipc.Error(e)})
			return &reported{e}
		}
		_ = c.show(paths)
		return e
	}})
	c.taskCommands(root)
	c.runCommands(root)
	c.settingsCommands(root)
	c.eventCommands(root)
	c.observeCommands(root)
	if e := root.Execute(); e != nil {
		if r, ok := e.(*reported); ok {
			return exitCode(ipc.Error(r.err))
		}
		v := ipc.Error(e)
		var domainError *model.Error
		if !errors.As(e, &domainError) {
			v.Code = "INVALID_ARGUMENT"
		}
		if c.json {
			_ = json.NewEncoder(out).Encode(ipc.Response{Version: 1, Error: v})
		} else {
			fmt.Fprintf(errout, "%s: %s\n", v.Code, v.Message)
		}
		return exitCode(v)
	}
	return 0
}

type reported struct{ err error }

func (e *reported) Error() string { return e.err.Error() }
func exitCode(e *model.Error) int {
	switch e.Code {
	case "INVALID_ARGUMENT":
		return 2
	case "APP_NOT_RUNNING":
		return 3
	case "NOT_FOUND":
		return 4
	case "CONFLICT", "STALE_REVISION":
		return 5
	case "IPC_TIMEOUT", "IPC_UNAVAILABLE", "PROTOCOL_MISMATCH":
		return 6
	default:
		return 1
	}
}
func (c *client) taskCommands(root *cobra.Command) {
	group := &cobra.Command{Use: "task", Short: "Create and manage tasks"}
	root.AddCommand(group)
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		return c.invoke("task.list", map[string]any{"limit": limit, "offset": offset})
	}}
	list.Flags().Int("limit", 100, "Maximum items")
	list.Flags().Int("offset", 0, "Items to skip")
	group.AddCommand(list)
	for _, op := range []string{"show", "remove", "enable", "disable", "run"} {
		op := op
		group.AddCommand(&cobra.Command{Use: op + " TASK", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			method := map[string]string{"show": "get", "remove": "remove", "enable": "set_enabled", "disable": "set_enabled", "run": "run"}[op]
			return c.invoke("task."+method, map[string]any{"task": a[0], "enabled": op == "enable"})
		}})
	}
	for _, op := range []string{"add", "update"} {
		op := op
		cmd := &cobra.Command{Use: op + " TASK [flags] -- EXECUTABLE [ARGS...]", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			in := model.TaskInput{Enabled: true, TimeoutMS: 1800000}
			expected := int64(0)
			if op == "update" {
				b, e := c.call("task.get", map[string]any{"task": a[0]})
				if e != nil {
					return e
				}
				var t model.Task
				if e = json.Unmarshal(b, &t); e != nil {
					return e
				}
				in = model.TaskInput{Name: t.Name, Description: t.Description, Executable: t.Executable, Args: t.Args, Cwd: t.Cwd, Enabled: t.Enabled, TimeoutMS: t.TimeoutMS, Schedule: t.Schedule}
				expected = t.Revision
			} else {
				in.Name = a[0]
				cwd, e := os.Getwd()
				if e != nil {
					return e
				}
				in.Cwd = cwd
				b, e := c.call("settings.get", nil)
				if e != nil {
					return e
				}
				var settings model.Settings
				if e = json.Unmarshal(b, &settings); e != nil {
					return e
				}
				in.TimeoutMS = settings.DefaultTimeoutMS
			}
			f := cmd.Flags()
			for name, dst := range map[string]*string{"name": &in.Name, "description": &in.Description, "cwd": &in.Cwd} {
				if f.Changed(name) {
					*dst, _ = f.GetString(name)
				}
			}
			if f.Changed("timeout") {
				d, _ := f.GetDuration("timeout")
				in.TimeoutMS = d.Milliseconds()
			}
			if f.Changed("disabled") {
				v, _ := f.GetBool("disabled")
				in.Enabled = !v
			}
			split := cmd.ArgsLenAtDash()
			if split >= 0 {
				if split != 1 || len(a) < 2 {
					return model.Err("INVALID_ARGUMENT", "Use TASK -- EXECUTABLE [ARGS...]")
				}
				in.Executable = a[1]
				in.Args = a[2:]
			} else if len(a) > 1 {
				return model.Err("INVALID_ARGUMENT", "Separate executable and arguments with --")
			}
			if op == "add" && in.Executable == "" {
				return model.Err("INVALID_ARGUMENT", "Executable is required after --")
			}
			if f.Changed("cron") && f.Changed("interval") {
				return model.Err("INVALID_ARGUMENT", "Choose either --cron or --interval")
			}
			clear, _ := f.GetBool("clear-schedule")
			if clear {
				in.Schedule = nil
			}
			if f.Changed("cron") || f.Changed("interval") {
				if clear {
					return model.Err("INVALID_ARGUMENT", "Cannot combine schedule and --clear-schedule")
				}
				typ := "cron"
				expr, _ := f.GetString("cron")
				if f.Changed("interval") {
					typ = "interval"
					expr, _ = f.GetString("interval")
				}
				zone, _ := f.GetString("timezone")
				in.Schedule = &model.Schedule{Type: typ, Expression: expr, Enabled: true, Timezone: zone}
			}
			if f.Changed("timezone") && in.Schedule != nil {
				in.Schedule.Timezone, _ = f.GetString("timezone")
			}
			if f.Changed("schedule-enabled") {
				if in.Schedule == nil {
					return model.Err("INVALID_ARGUMENT", "Task has no schedule")
				}
				in.Schedule.Enabled, _ = f.GetBool("schedule-enabled")
			}
			if f.Changed("env-file") {
				path, _ := f.GetString("env-file")
				env, e := readEnv(path)
				if e != nil {
					return e
				}
				in.EnvSet = env
			}
			in.EnvUnset, _ = f.GetStringArray("env-unset")
			if f.Changed("if-revision") {
				expected, _ = f.GetInt64("if-revision")
			}
			if op == "add" {
				return c.invoke("task.create", in)
			}
			return c.invoke("task.update", map[string]any{"task": a[0], "patch": in, "expected_revision": expected})
		}}
		f := cmd.Flags()
		f.String("name", "", "Task name")
		f.String("description", "", "Description")
		f.String("cwd", "", "Working directory")
		f.Duration("timeout", 30*time.Minute, "Task timeout")
		f.Bool("disabled", false, "Disable automatic runs")
		f.String("cron", "", "Five-field cron expression")
		f.String("interval", "", "Fixed interval, for example 30s")
		f.String("timezone", "Local", "IANA timezone")
		f.Bool("schedule-enabled", true, "Enable the schedule")
		f.Bool("clear-schedule", false, "Remove the schedule")
		f.String("env-file", "", "Read explicit KEY=VALUE environment file")
		f.StringArray("env-unset", nil, "Remove environment key")
		f.Int64("if-revision", 0, "Require task revision")
		group.AddCommand(cmd)
	}
}
func readEnv(path string) (map[string]string, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, model.Err("INVALID_ARGUMENT", "Cannot read environment file")
	}
	out := map[string]string{}
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" {
			return nil, model.Err("INVALID_ARGUMENT", fmt.Sprintf("Invalid environment file line %d", i+1))
		}
		out[k] = v
	}
	return out, nil
}
func (c *client) runCommands(root *cobra.Command) {
	g := &cobra.Command{Use: "run", Short: "Inspect and stop runs"}
	root.AddCommand(g)
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		task, _ := cmd.Flags().GetString("task")
		return c.invoke("run.list", map[string]any{"limit": limit, "offset": offset, "task": task})
	}}
	list.Flags().Int("limit", 20, "Maximum items")
	list.Flags().Int("offset", 0, "Items to skip")
	list.Flags().String("task", "", "Task ID or name")
	g.AddCommand(list)
	for _, op := range []string{"show", "stop"} {
		op := op
		g.AddCommand(&cobra.Command{Use: op + " RUN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
			method := "get"
			if op == "stop" {
				method = "stop"
			}
			return c.invoke("run."+method, map[string]any{"run_id": a[0]})
		}})
	}
	logs := &cobra.Command{Use: "logs RUN_ID", Short: "Read stdout or stderr", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		stream, _ := cmd.Flags().GetString("stream")
		offset, _ := cmd.Flags().GetInt64("offset")
		follow, _ := cmd.Flags().GetBool("follow")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		for {
			b, e := c.call("logs.read", map[string]any{"run_id": a[0], "stream": stream, "offset": offset, "limit": 65536})
			if e != nil {
				return e
			}
			var chunk app.LogChunk
			if e = json.Unmarshal(b, &chunk); e != nil {
				return e
			}
			if c.json {
				if e = c.show(b); e != nil {
					return e
				}
			} else {
				if _, e = c.out.Write(chunk.Data); e != nil {
					return e
				}
			}
			offset = chunk.NextOffset
			if chunk.EOF || (!follow && len(chunk.Data) < 65536) {
				return nil
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(250 * time.Millisecond):
			}
		}
	}}
	logs.Flags().String("stream", "stdout", "stdout or stderr")
	logs.Flags().Int64("offset", 0, "Byte offset")
	logs.Flags().Bool("follow", false, "Follow until the run ends")
	root.AddCommand(logs)
}
func (c *client) settingsCommands(root *cobra.Command) {
	g := &cobra.Command{Use: "settings", Short: "Manage App settings"}
	root.AddCommand(g)
	g.AddCommand(&cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return c.invoke("settings.get", nil) }})
	cmd := &cobra.Command{Use: "update", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		b, e := c.call("settings.get", nil)
		if e != nil {
			return e
		}
		var s model.Settings
		if e = json.Unmarshal(b, &s); e != nil {
			return e
		}
		f := cmd.Flags()
		if f.Changed("default-timeout") {
			d, _ := f.GetDuration("default-timeout")
			s.DefaultTimeoutMS = d.Milliseconds()
		}
		if f.Changed("max-concurrent") {
			s.MaxConcurrent, _ = f.GetInt("max-concurrent")
		}
		if f.Changed("retention-days") {
			s.RetentionDays, _ = f.GetInt("retention-days")
		}
		if f.Changed("log-limit") {
			s.LogLimitBytes, _ = f.GetInt64("log-limit")
		}
		for name, p := range map[string]*bool{"notify-failure": &s.NotifyFailure, "notify-success": &s.NotifySuccess, "notify-observer": &s.NotifyObserver} {
			if f.Changed(name) {
				*p, _ = f.GetBool(name)
			}
		}
		return c.invoke("settings.update", map[string]any{"patch": s, "expected_revision": s.Revision})
	}}
	f := cmd.Flags()
	f.Duration("default-timeout", 30*time.Minute, "Default task timeout")
	f.Int("max-concurrent", 4, "Maximum active tasks")
	f.Int("retention-days", 30, "History retention days")
	f.Int64("log-limit", 50*1024*1024, "Bytes per log stream")
	f.Bool("notify-failure", true, "Notify on task failure")
	f.Bool("notify-success", false, "Notify on success")
	f.Bool("notify-observer", true, "Notify on observation changes")
	g.AddCommand(cmd)
}
func (c *client) eventCommands(root *cobra.Command) {
	g := &cobra.Command{Use: "event", Short: "Review attention events"}
	root.AddCommand(g)
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, _ := cmd.Flags().GetBool("unread")
		return c.invoke("event.list", map[string]any{"unread": v})
	}}
	list.Flags().Bool("unread", false, "Only unacknowledged events")
	g.AddCommand(list)
	g.AddCommand(&cobra.Command{Use: "ack EVENT_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("event.ack", map[string]any{"event_id": a[0]})
	}})
}
