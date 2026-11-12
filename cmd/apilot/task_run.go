package main

import (
	"github.com/spf13/cobra"
)

func init() { taskCmd.AddCommand(newTaskRunCommand(commandClient)) }

func newTaskRunCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "run TASK", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("task.run", map[string]any{"task": a[0], "enabled": false})
	}}
}
