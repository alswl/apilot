package main

import (
	"strconv"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/spf13/cobra"
)

func init() {
	workbenchSyncCmd.AddCommand(&cobra.Command{
		Use:  "cancel CONNECTION_ID ATTEMPT_ID",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			attemptID, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || attemptID < 1 {
				return common.Err("INVALID_ARGUMENT", "A positive sync attempt ID is required")
			}
			return showWorkbench("workbench.sync.cancel", map[string]any{"id": args[0], "attempt_id": attemptID})
		},
	})
}
