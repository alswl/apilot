package main

import (
	"github.com/spf13/cobra"
)

func init() { taskCmd.AddCommand(newTaskRemoveCommand(commandClient)) }

func newTaskRemoveCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "remove TASK", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("task.remove", map[string]any{"task": a[0], "enabled": false})
	}}
}
