package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectionCmd.AddCommand(&cobra.Command{Use: "fields SCOPE_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.scope.fields", map[string]string{"id": args[0]})
		},
	})
}
