package main

import (
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/spf13/cobra"
)

var workbenchTaskCmd = &cobra.Command{Use: "task", Short: "Manage source tasks"}

func init() {
	group := workbenchTaskCmd
	var q common.WorkbenchQuery
	list := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error { return showWorkbench("workbench.task.list", q) }}
	list.Flags().StringVar(&q.Connection, "connection", "", "Connection ID")
	list.Flags().StringVar(&q.State, "state", "", "Normalized state")
	list.Flags().IntVar(&q.Limit, "limit", 50, "Maximum items")
	list.Flags().IntVar(&q.Offset, "offset", 0, "Items to skip")
	list.Flags().Int64Var(&q.Revision, "revision", 0, "Required task list revision")
	group.AddCommand(list, &cobra.Command{Use: "show ID", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		return showWorkbench("workbench.task.show", map[string]string{"id": args[0]})
	}})
	group.AddCommand(&cobra.Command{Use: "import FILE|-", Short: "Replace tasks in a Local connection from JSON", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return workbenchJSONFile(cmd, "workbench.task.import", args[0])
	}})
	workbenchCmd.AddCommand(group)
}
