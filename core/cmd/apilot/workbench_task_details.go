package main

import "github.com/spf13/cobra"

func init() {
	var segment, page, scopeID string
	cmd := &cobra.Command{Use: "detail TASK_ID", Short: "Read a task detail segment", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			params := map[string]string{"id": args[0], "segment": segment}
			if page != "" {
				params["page"] = page
			}
			if scopeID != "" {
				params["scope_id"] = scopeID
			}
			return showWorkbench("workbench.task.detail", params)
		}}
	cmd.Flags().StringVar(&segment, "segment", "fields", "Detail segment (top_fields reads the complete task header)")
	cmd.Flags().StringVar(&scopeID, "scope", "", "Expected saved Connector scope ID")
	cmd.Flags().StringVar(&page, "page", "", "Page key from the previous result")
	workbenchTaskCmd.AddCommand(cmd)
}
