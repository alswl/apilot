package common

import "github.com/alswl/apilot/connectors/protocol"

type WorkbenchConnectorNewScope struct {
	ID           string
	Context      protocol.ScopeContext
	Label        string
	DisplayName  string
	WriteEnabled bool
}

type WorkbenchConnectorStoredScope struct {
	WorkbenchScopeState
	Config         WorkbenchConnection
	ConnectorID    string
	Context        protocol.ScopeContext
	AuthGeneration int64
	AuthState      string
}

type WorkbenchConnectorSyncCommit struct {
	Scope      WorkbenchConnectorStoredScope
	Release    WorkbenchConnectorRelease
	PluginID   string
	Run        WorkbenchSyncRun
	Checkpoint string
	Records    map[string]WorkbenchTask
	Removed    map[string]bool
	Hidden     map[string]bool
}
