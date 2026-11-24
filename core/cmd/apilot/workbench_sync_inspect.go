package main

import "github.com/spf13/cobra"

func init() {
	workbenchSyncCmd.AddCommand(&cobra.Command{Use: "inspect SYNC_RUN_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.sync.inspect", map[string]string{"id": args[0]})
		},
	})
}
