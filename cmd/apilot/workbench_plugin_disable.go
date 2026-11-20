package main

import "github.com/spf13/cobra"

func init() {
	workbenchPluginCmd.AddCommand(&cobra.Command{
		Use:  "disable PLUGIN",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.plugin.set", map[string]any{"id": args[0], "enabled": false})
		},
	})
}
