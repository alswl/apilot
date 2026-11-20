package main

import (
	"github.com/spf13/cobra"
)

func init() { taskCmd.AddCommand(newTaskEnableCommand(commandClient)) }

func newTaskEnableCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "enable TASK", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("task.set_enabled", map[string]any{"task": a[0], "enabled": true})
	}}
}
