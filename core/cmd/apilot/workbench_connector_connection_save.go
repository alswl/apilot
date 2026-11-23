package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectorConnectionCmd.AddCommand(&cobra.Command{Use: "save FILE", Short: "Save selected scopes from a JSON file or stdin (-)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "workbench.connector.connection.save", args[0])
		},
	})
}
