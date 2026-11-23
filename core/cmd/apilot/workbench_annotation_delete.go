package main

import "github.com/spf13/cobra"

func init() {
	workbenchAnnotationCmd.AddCommand(&cobra.Command{
		Use: "delete TASK_ID ANNOTATION_ID", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showWorkbench("workbench.annotation.delete", map[string]string{"id": args[0], "annotation_id": args[1]})
		},
	})
}
