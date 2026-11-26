package main

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

var workbenchConnectorScopeCmd = &cobra.Command{Use: "scope", Short: "Read and edit a saved Connector scope"}

func init() { workbenchConnectorCmd.AddCommand(workbenchConnectorScopeCmd) }

func runWorkbenchConnectorScopeDraft(cmd *cobra.Command, args []string, action string) error {
	body, err := readWorkbenchJSON(cmd, args[1])
	if err != nil {
		return err
	}
	return showWorkbench("workbench.connector.scope."+action, struct {
		ID   string          `json:"id"`
		Body json.RawMessage `json:"body"`
	}{args[0], body})
}
