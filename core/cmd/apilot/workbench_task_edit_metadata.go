package main

import "github.com/spf13/cobra"

func init() {
	workbenchTaskCmd.AddCommand(&cobra.Command{Use: "edit-metadata TASK_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.task.edit-metadata", map[string]string{"id": args[0]})
		},
	})
	workbenchConnectionCmd.AddCommand(&cobra.Command{Use: "edit-metadata SCOPE_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.scope.edit-metadata", map[string]string{"id": args[0]})
		},
	})
}
