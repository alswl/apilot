package main

import (
	"github.com/spf13/cobra"
)

func init() { eventCmd.AddCommand(newEventListCommand(commandClient)) }

func newEventListCommand(c *client) *cobra.Command {
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		v, _ := cmd.Flags().GetBool("unread")
		return c.invoke("event.list", map[string]any{"unread": v})
	}}
	list.Flags().Bool("unread", false, "Only unacknowledged events")
	return list
}
