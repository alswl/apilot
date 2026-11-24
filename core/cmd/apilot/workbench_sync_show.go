package main

import "github.com/spf13/cobra"

func init() {
	workbenchSyncCmd.AddCommand(&cobra.Command{Use: "show CONNECTION_ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return showWorkbench("workbench.sync.show", map[string]string{"id": args[0]})
	}})
}
