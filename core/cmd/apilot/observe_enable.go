package main

import (
	"github.com/spf13/cobra"
)

func init() { observeCmd.AddCommand(newObserveEnableCommand(commandClient)) }

func newObserveEnableCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "enable OBSERVER", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("observer.set_enabled", map[string]any{"observer": a[0], "enabled": true})
	}}
}
