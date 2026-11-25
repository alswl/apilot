package main

import "github.com/spf13/cobra"

func init() {
	var authorization, field, search, page, scopeType string
	cmd := &cobra.Command{Use: "options CONNECTOR_ID", Short: "List choices for an authorized Connector configuration field", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			in := map[string]string{"id": args[0], "authorization_id": authorization, "field": field}
			if search != "" {
				in["search"] = search
			}
			if page != "" {
				in["page"] = page
			}
			if scopeType != "" {
				in["scope_type"] = scopeType
			}
			return showWorkbench("workbench.connector.options", in)
		}}
	cmd.Flags().StringVar(&authorization, "authorization", "", "Completed login ID")
	cmd.Flags().StringVar(&field, "field", "", "Configuration field ID")
	cmd.Flags().StringVar(&search, "search", "", "Find matching options")
	cmd.Flags().StringVar(&scopeType, "scope-type", "", "Declared Connector scope type")
	cmd.Flags().StringVar(&page, "page", "", "Page key from the previous result")
	_ = cmd.MarkFlagRequired("authorization")
	_ = cmd.MarkFlagRequired("field")
	workbenchConnectorCmd.AddCommand(cmd)
}
