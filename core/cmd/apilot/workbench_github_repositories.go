package main

import "github.com/spf13/cobra"

func init() {
	var authorization string
	cmd := &cobra.Command{
		Use: "repositories", Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return showWorkbench("workbench.github.repository.list", map[string]string{"authorization_id": authorization})
		},
	}
	cmd.Flags().StringVar(&authorization, "authorization", "", "Completed authorization ID")
	workbenchGitHubCmd.AddCommand(cmd)
}
