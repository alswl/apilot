package main

import "github.com/spf13/cobra"

func init() {
	workbenchAnnotationCmd.AddCommand(&cobra.Command{
		Use: "put FILE|-", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "workbench.annotation.put", args[0])
		},
	})
}
