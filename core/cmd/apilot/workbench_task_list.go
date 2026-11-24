package main

import (
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/spf13/cobra"
)

func init() {
	var q common.WorkbenchQuery
	var priority int
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if cmd.Flags().Changed("priority") {
			q.Priority = &priority
		} else {
			q.Priority = nil
		}
		return showWorkbench("workbench.task.list", q)
	}}
	list.Flags().StringVar(&q.ScopeID, "scope", "", "Scope ID")
	list.Flags().StringVar(&q.Source, "source", "", "Task source kind")
	list.Flags().StringVar(&q.Assignee, "assignee", "", "Normalized assignee")
	list.Flags().IntVar(&priority, "priority", 0, "Priority value")
	list.Flags().StringVar(&q.DueBefore, "due-before", "", "Latest date (YYYY-MM-DD)")
	list.Flags().StringVar(&q.DueAfter, "due-after", "", "Earliest date (YYYY-MM-DD)")
	list.Flags().StringVar(&q.Sort, "sort", "", "Sort field")
	list.Flags().BoolVar(&q.Descending, "descending", false, "Sort descending")
	list.Flags().StringVar(&q.Connection, "connection", "", "Connection ID")
	list.Flags().StringVar(&q.LabelID, "label-id", "", "Task label ID")
	list.Flags().StringVar(&q.State, "state", "", "Normalized state")
	list.Flags().IntVar(&q.Limit, "limit", 50, "Maximum items")
	list.Flags().IntVar(&q.Offset, "offset", 0, "Items to skip")
	list.Flags().Int64Var(&q.Revision, "revision", 0, "Required task list revision")
	workbenchTaskCmd.AddCommand(list)
}
