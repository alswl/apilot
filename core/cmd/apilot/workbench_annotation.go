package main

import "github.com/spf13/cobra"

var workbenchAnnotationCmd = &cobra.Command{Use: "annotation", Short: "Manage workbench task annotations"}

func init() { workbenchTaskCmd.AddCommand(workbenchAnnotationCmd) }
