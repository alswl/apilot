package main

import "github.com/spf13/cobra"

var automationCmd = &cobra.Command{Use: "automation", Short: "Manage local automation"}

func init() { rootCmd.AddCommand(automationCmd) }
