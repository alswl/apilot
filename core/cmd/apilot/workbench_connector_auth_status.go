package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectorCmd.AddCommand(&cobra.Command{Use: "auth-status ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connector.auth-status", map[string]string{"id": args[0]})
		},
	})
}
