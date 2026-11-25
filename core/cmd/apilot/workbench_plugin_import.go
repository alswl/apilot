package main

import (
	"path/filepath"

	"github.com/spf13/cobra"
)

func init() {
	workbenchPluginCmd.AddCommand(&cobra.Command{
		Use:   "import PACKAGE",
		Short: "Install or upgrade a signed local Connector package",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			return showWorkbench("workbench.plugin.import", map[string]string{"path": path})
		},
	})
}
