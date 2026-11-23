//go:build apilot_controlled_github

package main

import (
	"github.com/alswl/apilot/connectors/github-issues/internal/connector"
	"github.com/alswl/apilot/connectors/protocol"
)

func connectorHandler(dir string) (protocol.Handler, error) {
	c, err := connector.NewControlled(dir)
	if err != nil {
		return nil, err
	}
	return c.Handle, nil
}
