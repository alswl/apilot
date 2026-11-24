package main

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

var workbenchConnectionMappingCmd = &cobra.Command{Use: "mapping", Short: "Read, preview, and save scope mappings"}

func init() { workbenchConnectionCmd.AddCommand(workbenchConnectionMappingCmd) }

func runWorkbenchMapping(cmd *cobra.Command, args []string, action string) error {
	input, err := readWorkbenchJSON(cmd, args[1])
	if err != nil {
		return err
	}
	return showWorkbench("workbench.scope.mapping."+action, struct {
		ID   string          `json:"id"`
		Body json.RawMessage `json:"body"`
	}{args[0], input})
}
