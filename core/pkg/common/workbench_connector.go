package common

import (
	"github.com/alswl/apilot/connectors/protocol"
	"time"
)

// WorkbenchConnectorConnection is the account-level parent of one or more scopes.
type WorkbenchConnectorConnection struct {
	ID             string `json:"connection_id"`
	ConnectorID    string `json:"connector_id"`
	EndpointRef    string `json:"endpoint_ref"`
	AccountRef     string `json:"-"`
	AuthState      string `json:"auth_state"`
	AuthGeneration int64  `json:"-"`
}

type WorkbenchScopeState struct {
	ScopeID         string     `json:"scope_id"`
	ConnectionID    string     `json:"connection_id"`
	ScopeRef        string     `json:"scope_ref"`
	AccessState     string     `json:"access_state"`
	ConfigRevision  int64      `json:"config_revision"`
	MappingRevision int64      `json:"mapping_revision"`
	SnapshotVersion int64      `json:"snapshot_version"`
	LastSuccess     *time.Time `json:"last_success,omitempty"`
	LastFullSuccess *time.Time `json:"last_full_success,omitempty"`
	Checkpoint      string     `json:"-"`
}

type WorkbenchConnectorError struct {
	Kind          string     `json:"kind"`
	AffectedLevel string     `json:"affected_level,omitempty"`
	RetryAt       *time.Time `json:"retry_at,omitempty"`
	FieldPath     string     `json:"field_path,omitempty"`
}

const (
	WorkbenchConnectorCanceled        = "canceled"
	WorkbenchConnectorTimeout         = "timeout"
	WorkbenchConnectorProcessFailure  = "process_failure"
	WorkbenchConnectorProtocolFailure = "protocol_failure"
)

type WorkbenchSyncRun struct {
	ConnectorID     string     `json:"connector_id,omitempty"`
	ConnectionID    string     `json:"connection_id,omitempty"`
	RateLimitBucket string     `json:"rate_limit_bucket,omitempty"`
	LegacyID        int64      `json:"-"`
	ID              string     `json:"sync_run_id"`
	ScopeID         string     `json:"scope_id"`
	Trigger         string     `json:"trigger"`
	Mode            string     `json:"mode"`
	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
	DurationMS      *int64     `json:"duration_ms,omitempty"`
	FailedStage     string     `json:"failed_stage,omitempty"`
	ErrorKind       string     `json:"error_kind,omitempty"`
	RetryAt         *time.Time `json:"retry_at,omitempty"`
	Requests        int        `json:"requests"`
	Pages           int        `json:"pages"`
	Fetched         int        `json:"fetched"`
	Changed         int        `json:"changed"`
	RateLimits      int        `json:"rate_limits"`
	UsageComplete   bool       `json:"usage_complete"`
}

type WorkbenchConnectorRelease struct {
	ConnectorID     string `json:"connector_id"`
	ReleaseVersion  string `json:"release_version"`
	ProtocolVersion int    `json:"protocol_version"`
	PackageDigest   string `json:"package_digest"`
}

type WorkbenchConnectorLoginInput struct {
	ConnectorID  string `json:"connector_id"`
	ConnectionID string `json:"connection_id,omitempty"`
}

type WorkbenchConnectorAuthorization struct {
	ID              string    `json:"id"`
	ConnectorID     string    `json:"connector_id"`
	Status          string    `json:"status"`
	DisplayName     string    `json:"display_name,omitempty"`
	EndpointRef     string    `json:"endpoint_ref,omitempty"`
	VerificationURL string    `json:"verification_url,omitempty"`
	UserCode        string    `json:"user_code,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	ErrorKind       string    `json:"error_kind,omitempty"`
}

type WorkbenchConnectorSelection struct {
	Config       map[string]any `json:"config"`
	WriteEnabled bool           `json:"write_enabled,omitempty"`
}

type WorkbenchConnectorSaveInput struct {
	AuthorizationID       string                        `json:"authorization_id"`
	EndpointConfig        map[string]any                `json:"endpoint_config,omitempty"`
	EndpointSchemaVersion int                           `json:"endpoint_schema_version" minimum:"1"`
	ScopeSchemaVersion    int                           `json:"scope_schema_version" minimum:"1"`
	Scopes                []WorkbenchConnectorSelection `json:"scopes" minItems:"1" maxItems:"20"`
}

type WorkbenchConnectorScopeResult struct {
	ScopeID   string `json:"scope_id,omitempty"`
	ScopeRef  string `json:"scope_ref,omitempty"`
	Label     string `json:"label,omitempty"`
	Status    string `json:"status"`
	ErrorKind string `json:"error_kind,omitempty"`
}

type WorkbenchConnectorSaveResult struct {
	ConnectionID string                          `json:"connection_id,omitempty"`
	Scopes       []WorkbenchConnectorScopeResult `json:"scopes"`
}

type WorkbenchConnectorOverview struct {
	Connection WorkbenchConnectorConnection      `json:"connection"`
	Scopes     []WorkbenchConnectorScopeOverview `json:"scopes"`
}

type WorkbenchConnectorScopeOverview struct {
	WorkbenchScopeState
	Label   string            `json:"label"`
	LastRun *WorkbenchSyncRun `json:"last_run,omitempty"`
}

type WorkbenchTaskDetail struct {
	TaskID     string                         `json:"task_id"`
	ScopeID    string                         `json:"scope_id"`
	Segment    string                         `json:"segment"`
	Version    string                         `json:"version"`
	Fields     map[string]protocol.FieldValue `json:"fields,omitempty"`
	Items      []protocol.DetailItem          `json:"items,omitempty"`
	NextPage   string                         `json:"next_page,omitempty"`
	CapturedAt time.Time                      `json:"captured_at"`
	Cached     bool                           `json:"cached"`
}

type WorkbenchMappingInput struct {
	Revision        int64                           `json:"revision" minimum:"0"`
	Rules           map[string]protocol.MappingRule `json:"rules,omitempty"`
	RestoreDefaults bool                            `json:"restore_defaults,omitempty"`
	TaskID          string                          `json:"task_id,omitempty"`
}

type WorkbenchMappingResult struct {
	ScopeID     string                          `json:"scope_id"`
	Revision    int64                           `json:"revision"`
	Rules       map[string]protocol.MappingRule `json:"rules"`
	Issues      map[string]string               `json:"issues,omitempty"`
	Sample      *WorkbenchTask                  `json:"sample,omitempty"`
	PendingRead bool                            `json:"pending_read"`
}

type WorkbenchSyncQuery struct {
	ConnectorID  string `json:"connector_id,omitempty" query:"connector_id"`
	ConnectionID string `json:"connection_id,omitempty" query:"connection_id"`
	ScopeID      string `json:"scope_id,omitempty" query:"scope_id"`
	Mode         string `json:"mode,omitempty" query:"mode" enum:"full,delta"`
	Status       string `json:"status,omitempty" query:"status"`
	From         string `json:"from,omitempty" query:"from"`
	To           string `json:"to,omitempty" query:"to"`
	Limit        int    `json:"limit,omitempty" query:"limit" minimum:"1" maximum:"100" default:"50"`
	Offset       int    `json:"offset,omitempty" query:"offset" minimum:"0"`
}

type WorkbenchSyncUsage struct {
	ConnectorID  string `json:"connector_id"`
	ConnectionID string `json:"connection_id"`
	ScopeID      string `json:"scope_id"`
	Mode         string `json:"mode"`
	Runs         int    `json:"runs"`
	Success      int    `json:"success"`
	Failed       int    `json:"failed"`
	Running      int    `json:"running"`
	NoChanges    int    `json:"no_changes"`
	Requests     int    `json:"requests"`
	Pages        int    `json:"pages"`
	Fetched      int    `json:"fetched"`
	Changed      int    `json:"changed"`
	DurationMS   int64  `json:"duration_ms"`
	RateLimits   int    `json:"rate_limits"`
	PartialRuns  int    `json:"partial_runs"`
}
