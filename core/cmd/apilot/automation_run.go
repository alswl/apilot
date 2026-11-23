package main

import "github.com/spf13/cobra"

func init() {
	automationCmd.AddCommand(&cobra.Command{
		Use: "run ID", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return commandClient.invoke("automation.run", map[string]string{"id": args[0], "trigger": "manual"})
		},
	})
}
