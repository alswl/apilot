package main

import (
	"github.com/spf13/cobra"
)

func init() { observeCmd.AddCommand(newObserveShowCommand(commandClient)) }

func newObserveShowCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "show OBSERVER", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, a []string) error {
		return c.invoke("observer.get", map[string]any{"observer": a[0], "enabled": false})
	}}
}
