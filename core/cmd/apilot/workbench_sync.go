package main

import (
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/spf13/cobra"
)

var workbenchSyncCmd = &cobra.Command{Use: "sync", Short: "Run synchronization and query results"}

func init() {
	group := workbenchSyncCmd
	group.AddCommand(&cobra.Command{Use: "show CONNECTION_ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return showWorkbench("workbench.sync.show", map[string]string{"id": args[0]})
	}})
	var connection string
	run := &cobra.Command{Use: "run [SCOPE_ID]", Short: "Request a source refresh", Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if connection != "" && len(args) == 0 {
				return showWorkbench("workbench.connector.connection.sync", map[string]string{"id": connection, "mode": "auto"})
			}
			if connection != "" || len(args) != 1 {
				return common.Err("INVALID_ARGUMENT", "Provide a scope ID or --connection")
			}
			return showWorkbench("workbench.sync.run", map[string]string{"id": args[0]})
		}}
	run.Flags().StringVar(&connection, "connection", "", "Refresh all scopes of this parent connection")
	group.AddCommand(run)
	workbenchCmd.AddCommand(group)
}
