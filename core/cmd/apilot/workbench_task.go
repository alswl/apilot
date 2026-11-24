package main

import "github.com/spf13/cobra"

var workbenchTaskCmd = &cobra.Command{Use: "task", Short: "Manage source tasks"}

func init() { workbenchCmd.AddCommand(workbenchTaskCmd) }
