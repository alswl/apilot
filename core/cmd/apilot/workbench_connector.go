package main

import "github.com/spf13/cobra"

func init() {
	group := &cobra.Command{Use: "connector", Short: "Discover built-in connector types"}
	group.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		return showWorkbench("workbench.connector.list", nil)
	}})
	workbenchCmd.AddCommand(group)
}
