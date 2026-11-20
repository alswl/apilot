package main

import (
	"github.com/spf13/cobra"
)

func init() { eventCmd.AddCommand(newEventAckCommand(commandClient)) }

func newEventAckCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "ack EVENT_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("event.ack", map[string]any{"event_id": a[0]})
	}}
}
