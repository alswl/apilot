package main

import (
	"github.com/spf13/cobra"
)

func init() { settingsCmd.AddCommand(newSettingsShowCommand(commandClient)) }

func newSettingsShowCommand(c *client) *cobra.Command {
	return &cobra.Command{Use: "show", Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error { return c.invoke("settings.get", nil) }}
}
