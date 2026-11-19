package main

import (
	"strconv"

	"github.com/alswl/apilot/pkg/common"
	"github.com/spf13/cobra"
)

func init() {
	workbenchConnectionCmd.AddCommand(&cobra.Command{
		Use:  "resume CONNECTION_ID VERSION",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || version < 1 {
				return common.Err("INVALID_ARGUMENT", "A positive connection version is required")
			}
			return showWorkbench("workbench.connection.pause", map[string]any{"id": args[0], "version": version, "paused": false})
		},
	})
}
