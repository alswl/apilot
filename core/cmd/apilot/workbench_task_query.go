package main

import "github.com/spf13/cobra"

func init() {
	workbenchTaskCmd.AddCommand(&cobra.Command{
		Use:   "query FILE|-",
		Short: "Query and group the visible task snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "workbench.task.query", args[0])
		},
	})
}
