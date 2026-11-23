package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/alswl/apilot/connectors/github-issues/internal/connector"
	"github.com/alswl/apilot/connectors/protocol"
)

const releaseVersion = "0.1.0"

func main() {
	description := protocol.Description{
		ConnectorID:      "github-issues",
		ReleaseVersion:   releaseVersion,
		ProtocolVersions: []int{protocol.Version},
		Capabilities:     []string{"auth.login", "config.options", "tasks.sync.full", "tasks.sync.delta", "fields.describe", "tasks.detail", "tasks.edit_metadata", "tasks.mutate", "tasks.reconcile"},
		EndpointConfig: protocol.ConfigSchema{
			Version: 1,
			Schema:  json.RawMessage(`{"type":"object","additionalProperties":false}`),
		},
		ScopeConfig: protocol.ConfigSchema{
			Version: 1,
			Schema:  json.RawMessage(`{"type":"object","required":["repository_id"],"properties":{"repository_id":{"type":"string","pattern":"^[1-9][0-9]*$"}},"additionalProperties":false}`),
			Fields: map[string]protocol.ConfigField{
				"repository_id": {Title: "Repository", Control: "select", DynamicOptions: true},
			},
		},
	}
	ctx, cancel := protocol.RequestContext(os.Getenv("APILOT_CONNECTOR_DEADLINE"))
	defer cancel()
	if err := protocol.Serve(ctx, os.Args[1:], os.Stdin, os.Stdout, description, connector.New(os.Getenv("APILOT_CONNECTOR_DATA_DIR")).Handle); err != nil {
		fmt.Fprintln(os.Stderr, "GitHub Issues Connector:", err)
		os.Exit(1)
	}
}
