package main

import (
	"encoding/json"
	"os"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/spf13/cobra"
)

func (c *client) saveTaskInput(op string, cmd *cobra.Command, a []string) error {
	in := common.TaskInput{Enabled: true, TimeoutMS: 1800000}
	expected := int64(0)
	if op == "update" {
		b, e := c.call("task.get", map[string]any{"task": a[0]})
		if e != nil {
			return e
		}
		var t common.Task
		if e = json.Unmarshal(b, &t); e != nil {
			return e
		}
		in = common.TaskInput{Name: t.Name, Description: t.Description, Executable: t.Executable, Args: t.Args, Cwd: t.Cwd, Enabled: t.Enabled, TimeoutMS: t.TimeoutMS, Schedule: t.Schedule}
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
		var settings common.Settings
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
			return common.Err("INVALID_ARGUMENT", "Use TASK -- EXECUTABLE [ARGS...]")
		}
		in.Executable = a[1]
		in.Args = a[2:]
	} else if len(a) > 1 {
		return common.Err("INVALID_ARGUMENT", "Separate executable and arguments with --")
	}
	if op == "add" && in.Executable == "" {
		return common.Err("INVALID_ARGUMENT", "Executable is required after --")
	}
	if f.Changed("cron") && f.Changed("interval") {
		return common.Err("INVALID_ARGUMENT", "Choose either --cron or --interval")
	}
	clear, _ := f.GetBool("clear-schedule")
	if clear {
		in.Schedule = nil
	}
	if f.Changed("cron") || f.Changed("interval") {
		if clear {
			return common.Err("INVALID_ARGUMENT", "Cannot combine schedule and --clear-schedule")
		}
		typ := "cron"
		expr, _ := f.GetString("cron")
		if f.Changed("interval") {
			typ = "interval"
			expr, _ = f.GetString("interval")
		}
		zone, _ := f.GetString("timezone")
		in.Schedule = &common.Schedule{Type: typ, Expression: expr, Enabled: true, Timezone: zone}
	}
	if f.Changed("timezone") && in.Schedule != nil {
		in.Schedule.Timezone, _ = f.GetString("timezone")
	}
	if f.Changed("schedule-enabled") {
		if in.Schedule == nil {
			return common.Err("INVALID_ARGUMENT", "Task has no schedule")
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
}
func configureTaskInputFlags(cmd *cobra.Command) {
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
}
