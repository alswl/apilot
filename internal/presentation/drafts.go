package presentation

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

type FieldError struct{ Field, Code string }

func (e *FieldError) Error() string { return e.Code }
func duration(text, field, code string) (int64, *FieldError) {
	d, e := time.ParseDuration(text)
	if e != nil || d.Milliseconds() <= 0 {
		return 0, &FieldError{field, code}
	}
	return d.Milliseconds(), nil
}
func environment(set, unset string) (map[string]string, []string, *FieldError) {
	values := map[string]string{}
	var removed []string
	for _, line := range strings.Split(set, "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" || strings.ContainsRune(k, 0) || strings.ContainsRune(v, 0) {
			return nil, nil, &FieldError{"env", "environmentVariablesMustUseKeyValue"}
		}
		values[k] = v
	}
	for _, line := range strings.Split(unset, "\n") {
		if line != "" {
			removed = append(removed, line)
		}
	}
	return values, removed, nil
}
func arguments(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

type TaskDraft struct {
	Values                                               common.TaskInput
	ID                                                   string
	Revision                                             int64
	Mode, Plan                                           int
	Command, Args, Expression, Zone, Timeout, Env, Unset string
}

func NewTaskDraft(t *common.Task, defaults common.Settings, home string) TaskDraft {
	d := TaskDraft{Values: common.TaskInput{Enabled: true, Cwd: home, TimeoutMS: defaults.DefaultTimeoutMS}, Zone: "Local", Timeout: (time.Duration(defaults.DefaultTimeoutMS) * time.Millisecond).String()}
	if t == nil {
		return d
	}
	d.ID = t.ID
	d.Revision = t.Revision
	d.Values = common.TaskInput{Name: t.Name, Description: t.Description, Executable: t.Executable, Args: append([]string{}, t.Args...), Cwd: t.Cwd, Enabled: t.Enabled, TimeoutMS: t.TimeoutMS}
	d.Command = t.Executable
	d.Args = strings.Join(t.Args, "\n")
	d.Timeout = (time.Duration(t.TimeoutMS) * time.Millisecond).String()
	if t.Executable == "/bin/zsh" && len(t.Args) == 2 && t.Args[0] == "-lc" {
		d.Mode = 1
		d.Command = t.Args[1]
		d.Args = ""
	}
	if t.Schedule != nil {
		cp := *t.Schedule
		d.Values.Schedule = &cp
		d.Expression = cp.Expression
		d.Zone = cp.Timezone
		d.Plan = 1
		if cp.Type == "interval" {
			d.Plan = 2
		}
	}
	return d
}
func (d TaskDraft) Input() (common.TaskInput, *FieldError) {
	in := d.Values
	var e *FieldError
	in.EnvSet, in.EnvUnset, e = environment(d.Env, d.Unset)
	if e != nil {
		return common.TaskInput{}, e
	}
	in.TimeoutMS, e = duration(d.Timeout, "timeout", "timeoutMustBePositiveForExample30M")
	if e != nil {
		return common.TaskInput{}, e
	}
	in.Executable = d.Command
	in.Args = arguments(d.Args)
	if d.Mode == 1 {
		in.Executable = "/bin/zsh"
		in.Args = []string{"-lc", d.Command}
	}
	in.Schedule = nil
	if d.Plan > 0 {
		kind := "cron"
		if d.Plan == 2 {
			kind = "interval"
		}
		in.Schedule = &common.Schedule{Type: kind, Expression: d.Expression, Timezone: d.Zone, Enabled: true}
		if d.Values.Schedule != nil {
			in.Schedule.ID = d.Values.Schedule.ID
			in.Schedule.Enabled = d.Values.Schedule.Enabled
		}
	}
	return in, nil
}

type ObserverDraft struct {
	Values                              common.ObserverInput
	ID                                  string
	Revision                            int64
	Args, Interval, Timeout, Env, Unset string
	Format                              int
}

func NewObserverDraft(v *common.Observer, home string) ObserverDraft {
	d := ObserverDraft{Values: common.ObserverInput{Cwd: home, IntervalMS: 30000, TimeoutMS: 10000, Enabled: true, OutputMode: "text"}}
	if v != nil {
		d.ID = v.ID
		d.Revision = v.Revision
		d.Values = common.ObserverInput{Name: v.Name, Executable: v.Executable, Args: append([]string{}, v.Args...), Cwd: v.Cwd, IntervalMS: v.IntervalMS, TimeoutMS: v.TimeoutMS, Enabled: v.Enabled, OutputMode: v.OutputMode}
	}
	d.Args = strings.Join(d.Values.Args, "\n")
	d.Interval = (time.Duration(d.Values.IntervalMS) * time.Millisecond).String()
	d.Timeout = (time.Duration(d.Values.TimeoutMS) * time.Millisecond).String()
	if d.Values.OutputMode == "json" {
		d.Format = 1
	}
	return d
}
func (d ObserverDraft) Input() (common.ObserverInput, *FieldError) {
	in := d.Values
	var e *FieldError
	in.IntervalMS, e = duration(d.Interval, "interval", "invalidCheckInterval")
	if e != nil {
		return common.ObserverInput{}, e
	}
	in.TimeoutMS, e = duration(d.Timeout, "timeout", "invalidTimeout")
	if e != nil {
		return common.ObserverInput{}, e
	}
	in.EnvSet, in.EnvUnset, e = environment(d.Env, d.Unset)
	if e != nil {
		return common.ObserverInput{}, e
	}
	in.Args = arguments(d.Args)
	in.OutputMode = "text"
	if d.Format == 1 {
		in.OutputMode = "json"
	}
	return in, nil
}

type SettingsDraft struct {
	Values                    common.Settings
	Timeout, Max, Days, Limit string
}

func NewSettingsDraft(v common.Settings) SettingsDraft {
	return SettingsDraft{v, (time.Duration(v.DefaultTimeoutMS) * time.Millisecond).String(), strconv.Itoa(v.MaxConcurrent), strconv.Itoa(v.RetentionDays), strconv.FormatInt(v.LogLimitBytes/(1024*1024), 10)}
}
func (d SettingsDraft) Settings() (common.Settings, *FieldError) {
	v := d.Values
	var err error
	var e *FieldError
	v.DefaultTimeoutMS, e = duration(d.Timeout, "timeout", "invalidDefaultTimeout")
	if e != nil {
		return v, e
	}
	if v.MaxConcurrent, err = strconv.Atoi(d.Max); err != nil {
		return v, &FieldError{"max", "concurrentTasksMustBeAnInteger"}
	}
	if v.RetentionDays, err = strconv.Atoi(d.Days); err != nil {
		return v, &FieldError{"days", "retentionDaysMustBeAnInteger"}
	}
	l, err := strconv.ParseInt(d.Limit, 10, 64)
	if err != nil || l > math.MaxInt64/(1024*1024) || l < 0 {
		return v, &FieldError{"limit", "logLimitMustBeAnInteger"}
	}
	v.LogLimitBytes = l * 1024 * 1024
	return v, nil
}
