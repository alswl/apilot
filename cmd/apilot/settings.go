package main

import (
	"github.com/spf13/cobra"
)

var settingsCmd = &cobra.Command{Use: "settings", Short: "Manage App settings"}

func init() { rootCmd.AddCommand(settingsCmd) }
