package main

import (
	"github.com/alswl/apilot/pkg/common"
	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newVersionCommand(commandClient)) }

func newVersionCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "version", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
		return c.show(map[string]any{"app_version": common.Version, "protocol_version": 1})
	}}
}
