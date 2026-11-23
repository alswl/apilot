package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/alswl/apilot/connectors/protocol"
)

const releaseVersion = "0.1.0"

func main() {
	description := protocol.Description{
		ConnectorID:      "github-issues",
		ReleaseVersion:   releaseVersion,
		ProtocolVersions: []int{protocol.Version},
		Capabilities:     []string{},
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
	if err := protocol.Serve(context.Background(), os.Args[1:], os.Stdin, os.Stdout, description, nil); err != nil {
		fmt.Fprintln(os.Stderr, "GitHub Issues Connector:", err)
		os.Exit(1)
	}
}
