package main

import (
	"github.com/spf13/cobra"
)

func init() { taskCmd.AddCommand(newTaskListCommand(commandClient)) }

func newTaskListCommand(c *client) *cobra.Command {
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, a []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		return c.invoke("task.list", map[string]any{"limit": limit, "offset": offset})
	}}
	list.Flags().Int("limit", 100, "Maximum items")
	list.Flags().Int("offset", 0, "Items to skip")
	return list
}
