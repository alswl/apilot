package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectorScopeCmd.AddCommand(&cobra.Command{Use: "save SCOPE_ID FILE", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkbenchConnectorScopeDraft(cmd, args, "save")
		},
	})
}
