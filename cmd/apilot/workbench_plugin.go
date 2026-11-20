package main

import "github.com/spf13/cobra"

var workbenchPluginCmd = &cobra.Command{Use: "plugin", Short: "Manage task source plugins"}

func init() { workbenchCmd.AddCommand(workbenchPluginCmd) }
