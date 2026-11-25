package common

import "github.com/alswl/apilot/connectors/protocol"

type WorkbenchConnectorNewScope struct {
	ID             string
	Context        protocol.ScopeContext
	Label          string
	DisplayName    string
	WriteEnabled   bool
	AppendExisting bool
}

type WorkbenchConnectorStoredScope struct {
	WorkbenchScopeState
	Config         WorkbenchConnection
	ConnectorID    string
	Context        protocol.ScopeContext
	AuthGeneration int64
	AuthState      string
	Mapping        map[string]protocol.MappingRule
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

type WorkbenchConnectorOperationBinding struct {
	Mutation        protocol.MutationParams `json:"mutation"`
	ConfigRevision  int64                   `json:"config_revision"`
	MappingRevision int64                   `json:"mapping_revision"`
	AuthGeneration  int64                   `json:"auth_generation"`
	PackageDigest   string                  `json:"package_digest"`
}
