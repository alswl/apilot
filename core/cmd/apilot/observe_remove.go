package main

import (
	"github.com/spf13/cobra"
)

func init() { observeCmd.AddCommand(newObserveRemoveCommand(commandClient)) }

func newObserveRemoveCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "remove OBSERVER", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("observer.remove", map[string]any{"observer": a[0], "enabled": false})
	}}
}
