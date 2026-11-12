package main

import (
	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newStatusCommand(commandClient)) }

func newStatusCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return c.invoke("status", nil) }}
}
