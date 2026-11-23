package main

import "github.com/spf13/cobra"

func init() {
	workbenchAnnotationCmd.AddCommand(&cobra.Command{
		Use: "list TASK_ID", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showWorkbench("workbench.annotation.list", map[string]string{"id": args[0]})
		},
	})
}
