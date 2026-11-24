//go:build !apilot_controlled_github

package main

import (
	"github.com/alswl/apilot/connectors/github-issues/internal/connector"
	"github.com/alswl/apilot/connectors/protocol"
)

func connectorHandler(dir string) (protocol.Handler, error) { return connector.New(dir).Handle, nil }
