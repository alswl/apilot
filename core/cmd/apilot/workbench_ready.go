package main

import "github.com/spf13/cobra"

func init() {
	workbenchCmd.AddCommand(&cobra.Command{
		Use: "ready", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return showWorkbench("workbench.ready", nil) },
	})
}
