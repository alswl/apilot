package main

import "github.com/spf13/cobra"

var workbenchConnectorCmd = &cobra.Command{Use: "connector", Short: "Discover and configure task connectors"}

func init() {
	group := workbenchConnectorCmd
	group.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		return showWorkbench("workbench.connector.list", nil)
	}})
	workbenchCmd.AddCommand(group)
}
