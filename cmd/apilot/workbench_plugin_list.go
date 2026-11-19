package main

import "github.com/spf13/cobra"

func init() {
	workbenchPluginCmd.AddCommand(&cobra.Command{
		Use:  "list",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.plugin.list", nil)
		},
	})
}
