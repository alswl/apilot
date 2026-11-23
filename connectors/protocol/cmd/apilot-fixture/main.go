package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/alswl/apilot/connectors/protocol"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "browser" {
		if err := fixtureBrowser(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	description := protocol.Description{
		ConnectorID:      "fixture",
		ReleaseVersion:   "0.1.0",
		ProtocolVersions: []int{protocol.Version},
		Capabilities:     []string{"auth.login", "config.options", "tasks.sync.full", "tasks.sync.delta"},
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
	ctx, cancel := protocol.RequestContext(os.Getenv("APILOT_CONNECTOR_DEADLINE"))
	defer cancel()
	if err := protocol.Serve(ctx, os.Args[1:], os.Stdin, os.Stdout, description, fixtureHandler); err != nil {
		fmt.Fprintln(os.Stderr, "Apilot fixture:", err)
		os.Exit(1)
	}
}
