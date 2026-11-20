package main

import (
	"github.com/spf13/cobra"
)

func init() { runCmd.AddCommand(newRunStopCommand(commandClient)) }

func newRunStopCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "stop RUN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("run.stop", map[string]any{"run_id": a[0]})
	}}
}
