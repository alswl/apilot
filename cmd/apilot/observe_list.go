package main

import (
	"github.com/spf13/cobra"
)

func init() { observeCmd.AddCommand(newObserveListCommand(commandClient)) }

func newObserveListCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return c.invoke("observer.list", nil) }}
}
