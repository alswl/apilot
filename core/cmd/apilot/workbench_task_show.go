package main

import "github.com/spf13/cobra"

func init() {
	workbenchTaskCmd.AddCommand(&cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return showWorkbench("workbench.task.show", map[string]string{"id": args[0]})
	}})
}
