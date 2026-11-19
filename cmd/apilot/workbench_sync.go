package main

import "github.com/spf13/cobra"

func init() {
	group := &cobra.Command{Use: "sync", Short: "Query workbench sync results"}
	group.AddCommand(&cobra.Command{Use: "show CONNECTION_ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return showWorkbench("workbench.sync.show", map[string]string{"id": args[0]})
	}})
	workbenchCmd.AddCommand(group)
}
