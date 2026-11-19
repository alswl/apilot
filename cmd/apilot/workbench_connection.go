package main

import "github.com/spf13/cobra"

func init() {
	group := &cobra.Command{Use: "connection", Short: "Query workbench connections"}
	group.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		return showWorkbench("workbench.connection.list", nil)
	}})
	workbenchCmd.AddCommand(group)
}
