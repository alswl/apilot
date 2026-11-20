package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectionCmd.AddCommand(&cobra.Command{
		Use:  "notes CONNECTION_ID",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connection.notes", map[string]string{"id": args[0]})
		},
	})
}
