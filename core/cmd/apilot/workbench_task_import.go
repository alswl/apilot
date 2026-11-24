package main

import "github.com/spf13/cobra"

func init() {
	workbenchTaskCmd.AddCommand(&cobra.Command{Use: "import FILE|-", Short: "Replace tasks in a Local connection from JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return workbenchJSONFile(cmd, "workbench.task.import", args[0])
	}})
}
