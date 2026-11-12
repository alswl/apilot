package main

import "github.com/spf13/cobra"

func init() { observeCmd.AddCommand(newObserveAddCommand(commandClient)) }

func newObserveAddCommand(c *client) *cobra.Command {
	cmd := &cobra.Command{Use: "add OBSERVER -- EXECUTABLE [ARGS...]", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, a []string) error { return c.saveObserveInput("add", cmd, a) }}
	configureObserveInputFlags(cmd)
	return cmd
}
