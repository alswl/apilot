package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectorConnectionCmd.AddCommand(&cobra.Command{Use: "show CONNECTION_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connector.connection.show", map[string]string{"id": args[0]})
		},
	})
}
