package main

import "github.com/spf13/cobra"

func init() {
	workbenchLabelCmd.AddCommand(&cobra.Command{
		Use: "list", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return showWorkbench("workbench.label.list", nil) },
	})
}
