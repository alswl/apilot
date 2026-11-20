package main

import (
	"github.com/spf13/cobra"
)

func init() { taskCmd.AddCommand(newTaskShowCommand(commandClient)) }

func newTaskShowCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "show TASK", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("task.get", map[string]any{"task": a[0], "enabled": false})
	}}
}
