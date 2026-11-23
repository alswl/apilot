package main

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

func init() {
	group := &cobra.Command{Use: "mapping", Short: "Read, preview, and save scope mappings"}
	group.AddCommand(&cobra.Command{Use: "show SCOPE_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.scope.mapping", map[string]string{"id": args[0]})
		},
	})
	for _, action := range []string{"preview", "save"} {
		group.AddCommand(&cobra.Command{Use: action + " SCOPE_ID FILE|-", Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				input, err := readWorkbenchJSON(cmd, args[1])
				if err != nil {
					return err
				}
				return showWorkbench("workbench.scope.mapping."+action, struct {
					ID   string          `json:"id"`
					Body json.RawMessage `json:"body"`
				}{args[0], input})
			},
		})
	}
	workbenchConnectionCmd.AddCommand(group)
}
