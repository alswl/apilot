package main

import "github.com/spf13/cobra"

var workbenchSyncCmd = &cobra.Command{Use: "sync", Short: "Run synchronization and query results"}

func init() {
	group := workbenchSyncCmd
	group.AddCommand(&cobra.Command{Use: "show CONNECTION_ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return showWorkbench("workbench.sync.show", map[string]string{"id": args[0]})
	}})
	group.AddCommand(&cobra.Command{Use: "run CONNECTION_ID", Short: "Request a source refresh", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return showWorkbench("workbench.sync.run", map[string]string{"id": args[0]})
	}})
	workbenchCmd.AddCommand(group)
}
