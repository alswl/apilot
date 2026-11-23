package main

import "github.com/spf13/cobra"

func init() {
	workbenchGitHubAuthCmd.AddCommand(&cobra.Command{
		Use: "show ID", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showWorkbench("workbench.github.authorization.show", map[string]string{"id": args[0]})
		},
	})
}
