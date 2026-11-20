package main

import "github.com/spf13/cobra"

func init() {
	operation := &cobra.Command{Use: "operation", Short: "Submit and check task operations"}
	operation.AddCommand(
		&cobra.Command{Use: "submit FILE|-", Short: "Submit a task operation from JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			return workbenchJSONFile(cmd, "workbench.operation.submit", args[0])
		}},
		&cobra.Command{Use: "show REQUEST_ID", Short: "Show a task operation receipt", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.operation.show", map[string]string{"id": args[0]})
		}},
		&cobra.Command{Use: "reconcile REQUEST_ID", Short: "Check an uncertain operation against its source", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.operation.reconcile", map[string]string{"id": args[0]})
		}},
	)
	workbenchTaskCmd.AddCommand(operation)
}
