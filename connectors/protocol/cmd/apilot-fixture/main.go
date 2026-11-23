package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/alswl/apilot/connectors/protocol"
)

func main() {
	description := protocol.Description{
		ConnectorID:      "fixture",
		ReleaseVersion:   "0.1.0",
		ProtocolVersions: []int{protocol.Version},
		Capabilities:     []string{},
		EndpointConfig: protocol.ConfigSchema{
			Version: 1,
			Schema:  json.RawMessage(`{"type":"object","additionalProperties":false}`),
		},
		ScopeConfig: protocol.ConfigSchema{
			Version: 1,
			Schema:  json.RawMessage(`{"type":"object","required":["project"],"properties":{"project":{"type":"string","enum":["PAY","OPS"]}},"additionalProperties":false}`),
			Fields: map[string]protocol.ConfigField{
				"project": {Title: "Fixture project", Control: "select"},
			},
		},
	}
	if err := protocol.Serve(context.Background(), os.Args[1:], os.Stdin, os.Stdout, description, nil); err != nil {
		fmt.Fprintln(os.Stderr, "Apilot fixture:", err)
		os.Exit(1)
	}
}
