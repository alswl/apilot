package main

import "github.com/spf13/cobra"

func init() {
	automationCmd.AddCommand(&cobra.Command{
		Use: "snapshot", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return commandClient.invoke("automation.snapshot", nil) },
	})
}
