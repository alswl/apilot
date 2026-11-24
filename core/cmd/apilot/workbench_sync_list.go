package main

import (
	"github.com/spf13/cobra"

	"github.com/alswl/apilot/core/pkg/common"
)

func init() {
	workbenchSyncCmd.AddCommand(workbenchSyncQueryCommand("list", "List persisted sync runs"))
}

func workbenchSyncQueryCommand(name, short string) *cobra.Command {
	var q common.WorkbenchSyncQuery
	cmd := &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return showWorkbench("workbench.sync."+name, q) }}
	flags := cmd.Flags()
	flags.StringVar(&q.ConnectorID, "connector", "", "Connector ID")
	flags.StringVar(&q.ConnectionID, "connection", "", "Parent connection ID")
	flags.StringVar(&q.ScopeID, "scope", "", "Scope ID")
	flags.StringVar(&q.Mode, "mode", "", "Sync mode (full or delta)")
	flags.StringVar(&q.Status, "status", "", "Run status")
	flags.StringVar(&q.From, "from", "", "Start time, inclusive (RFC3339)")
	flags.StringVar(&q.To, "to", "", "End time, exclusive (RFC3339)")
	flags.IntVar(&q.Limit, "limit", 50, "Maximum rows")
	flags.IntVar(&q.Offset, "offset", 0, "Rows to skip")
	return cmd
}
