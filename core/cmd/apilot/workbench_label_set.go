package main

import "github.com/spf13/cobra"

func init() {
	workbenchLabelCmd.AddCommand(&cobra.Command{
		Use: "set FILE|-", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "workbench.label.set", args[0])
		},
	})
}
