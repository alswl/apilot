package main

import "github.com/spf13/cobra"

func init() {
	automationCmd.AddCommand(&cobra.Command{
		Use: "set-automatic FILE|-", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "automation.set_automatic", args[0])
		},
	})
}
