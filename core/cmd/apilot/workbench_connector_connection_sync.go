package main

import "github.com/spf13/cobra"

func init() {
	var full bool
	cmd := &cobra.Command{Use: "sync CONNECTION_ID", Short: "Refresh each scope of a Connector connection", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			mode := "auto"
			if full {
				mode = "full"
			}
			return showWorkbench("workbench.connector.connection.sync", map[string]string{"id": args[0], "mode": mode})
		}}
	cmd.Flags().BoolVar(&full, "full", false, "Run full scope reconciliation")
	workbenchConnectorConnectionCmd.AddCommand(cmd)
}
