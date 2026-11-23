package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectorCmd.AddCommand(&cobra.Command{Use: "cancel-login ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connector.cancel-login", map[string]string{"id": args[0]})
		},
	})
}
