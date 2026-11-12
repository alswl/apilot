package main

import "github.com/spf13/cobra"

func init() { observeCmd.AddCommand(newObserveUpdateCommand(commandClient)) }

func newObserveUpdateCommand(c *client) *cobra.Command {
	cmd := &cobra.Command{Use: "update OBSERVER -- EXECUTABLE [ARGS...]", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error { return c.saveObserveInput("update", cmd, a) }}
	configureObserveInputFlags(cmd)
	return cmd
}
