package main

import (
	"github.com/spf13/cobra"
)

func init() { observeCmd.AddCommand(newObserveDisableCommand(commandClient)) }

func newObserveDisableCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "disable OBSERVER", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("observer.set_enabled", map[string]any{"observer": a[0], "enabled": false})
	}}
}
