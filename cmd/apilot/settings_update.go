package main

import (
	"encoding/json"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/spf13/cobra"
)

func init() { settingsCmd.AddCommand(newSettingsUpdateCommand(commandClient)) }

func newSettingsUpdateCommand(c *client) *cobra.Command {
	cmd := &cobra.Command{Use: "update", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		b, e := c.call("settings.get", nil)
		if e != nil {
			return e
		}
		var s common.Settings
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
	return cmd
}
