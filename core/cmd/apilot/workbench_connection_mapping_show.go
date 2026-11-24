package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectionMappingCmd.AddCommand(&cobra.Command{Use: "show SCOPE_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.scope.mapping", map[string]string{"id": args[0]})
		}})
}
