package main

import "github.com/spf13/cobra"

func init() {
	workbenchGitHubAuthCmd.AddCommand(&cobra.Command{
		Use: "start", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showWorkbench("workbench.github.authorization.start", nil)
		},
	})
}
