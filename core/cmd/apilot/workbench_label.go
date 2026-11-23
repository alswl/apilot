package main

import "github.com/spf13/cobra"

var workbenchLabelCmd = &cobra.Command{Use: "label", Short: "Manage workbench task labels"}

func init() { workbenchCmd.AddCommand(workbenchLabelCmd) }
