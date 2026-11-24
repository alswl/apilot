package main

import "github.com/spf13/cobra"

func init() {
	workbenchConnectionMappingCmd.AddCommand(&cobra.Command{Use: "preview SCOPE_ID FILE|-", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorkbenchMapping(cmd, args, "preview")
		}})
}
