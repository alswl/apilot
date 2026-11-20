package main

import "github.com/spf13/cobra"

func init() {
	workbenchTaskCmd.AddCommand(&cobra.Command{
		Use:  "create FILE|-",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "workbench.task.create", args[0])
		},
	})
}
