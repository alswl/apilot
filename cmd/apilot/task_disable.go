package main

import (
	"github.com/spf13/cobra"
)

func init() { taskCmd.AddCommand(newTaskDisableCommand(commandClient)) }

func newTaskDisableCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "disable TASK", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("task.set_enabled", map[string]any{"task": a[0], "enabled": false})
	}}
}
