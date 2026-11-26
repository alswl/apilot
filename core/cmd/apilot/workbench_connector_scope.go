package main

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

func init() {
	group := &cobra.Command{Use: "scope", Short: "Read and edit a saved Connector scope"}
	workbenchConnectorCmd.AddCommand(group)
	group.AddCommand(&cobra.Command{Use: "show SCOPE_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return showWorkbench("workbench.connector.scope.show", map[string]string{"id": args[0]})
		},
	})
	for _, action := range []string{"preview", "save"} {
		group.AddCommand(&cobra.Command{Use: action + " SCOPE_ID FILE", Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				body, err := readWorkbenchJSON(cmd, args[1])
				if err != nil {
					return err
				}
				return showWorkbench("workbench.connector.scope."+action, struct {
					ID   string          `json:"id"`
					Body json.RawMessage `json:"body"`
				}{args[0], body})
			},
		})
	}
	var revision int64
	var field, search, page, scopeType, config string
	options := &cobra.Command{Use: "options SCOPE_ID", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			in := map[string]any{"id": args[0], "revision": revision, "field": field}
			for key, value := range map[string]string{"search": search, "page": page, "scope_type": scopeType, "scope_config": config} {
				if value != "" {
					in[key] = value
				}
			}
			return showWorkbench("workbench.connector.scope.options", in)
		},
	}
	options.Flags().Int64Var(&revision, "revision", 0, "Saved scope revision")
	options.Flags().StringVar(&field, "field", "", "Configuration field ID")
	options.Flags().StringVar(&search, "search", "", "Find matching options")
	options.Flags().StringVar(&page, "page", "", "Page key from the previous result")
	options.Flags().StringVar(&scopeType, "scope-type", "", "Declared Connector scope type")
	options.Flags().StringVar(&config, "scope-config", "", "Draft scope configuration as JSON")
	_ = options.MarkFlagRequired("revision")
	_ = options.MarkFlagRequired("field")
	group.AddCommand(options)
}
