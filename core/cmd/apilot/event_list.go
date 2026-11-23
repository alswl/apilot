package main

import (
	"github.com/spf13/cobra"
)

func init() { eventCmd.AddCommand(newEventListCommand(commandClient)) }

func newEventListCommand(c *client) *cobra.Command {
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, _ := cmd.Flags().GetBool("unread")
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		return c.invoke("event.list", map[string]any{"unread": v, "limit": limit, "offset": offset})
	}}
	list.Flags().Bool("unread", false, "Only unacknowledged events")
	list.Flags().Int("limit", 0, "Maximum items (default: server limit)")
	list.Flags().Int("offset", 0, "Items to skip")
	return list
}
