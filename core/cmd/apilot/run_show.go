package main

import (
	"github.com/spf13/cobra"
)

func init() { runCmd.AddCommand(newRunShowCommand(commandClient)) }

func newRunShowCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "show RUN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error { return c.invoke("run.get", map[string]any{"run_id": a[0]}) }}
}
