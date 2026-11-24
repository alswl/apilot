package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	Version              = 1
	ManifestVersion      = 1
	MaxFrame             = 8 << 20
	MaxLoginNotification = 64 << 10
)

type Manifest struct {
	ManifestVersion  int    `json:"manifest_version"`
	ConnectorID      string `json:"connector_id"`
	ReleaseVersion   string `json:"release_version"`
	ProtocolVersions []int  `json:"protocol_versions"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
	Entrypoint       string `json:"entrypoint"`
	EntrypointSHA256 string `json:"entrypoint_sha256"`
}

type Description struct {
	ConnectorID          string              `json:"connector_id"`
	ReleaseVersion       string              `json:"release_version"`
	ProtocolVersions     []int               `json:"protocol_versions"`
	Capabilities         []string            `json:"capabilities"`
	SyncRateLimitBuckets map[string][]string `json:"sync_rate_limit_buckets,omitempty"`
	EndpointConfig       ConfigSchema        `json:"endpoint_config"`
	ScopeConfig          ConfigSchema        `json:"scope_config"`
}

type ConfigSchema struct {
	Version int                    `json:"version"`
	Schema  json.RawMessage        `json:"schema"`
	Fields  map[string]ConfigField `json:"fields,omitempty"`
}

type ConfigField struct {
	Title          string   `json:"title"`
	Control        string   `json:"control"`
	DynamicOptions bool     `json:"dynamic_options,omitempty"`
	DependsOn      []string `json:"depends_on,omitempty"`
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *string         `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type BrowserNotification struct {
	RequestID string `json:"request_id"`
	URL       string `json:"url"`
	UserCode  string `json:"user_code,omitempty"`
}

type ErrorKind string

const (
	AuthRequired      ErrorKind = "auth_required"
	AccessDenied      ErrorKind = "access_denied"
	InvalidConfig     ErrorKind = "invalid_config"
	InvalidCheckpoint ErrorKind = "invalid_checkpoint"
	RateLimited       ErrorKind = "rate_limited"
	Temporary         ErrorKind = "temporary"
	Unsupported       ErrorKind = "unsupported"
)

type RPCError struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    ErrorData `json:"data"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("Connector RPC error %d (%s)", e.Code, e.Data.Kind)
}

type ErrorData struct {
	Reason          string     `json:"reason,omitempty"`
	Kind            ErrorKind  `json:"kind"`
	AffectedLevel   string     `json:"affected_level,omitempty"`
	RetryAt         *time.Time `json:"retry_at,omitempty"`
	RateLimitBucket string     `json:"rate_limit_bucket,omitempty"`
	FieldPath       string     `json:"field_path,omitempty"`
	Diagnostic      string     `json:"diagnostic,omitempty"`
	Usage           *Usage     `json:"usage,omitempty"`
}

type Usage struct {
	Requests   int `json:"requests"`
	Pages      int `json:"pages"`
	Received   int `json:"received"`
	RateLimits int `json:"rate_limits"`
}

type FieldType string

const (
	FieldText     FieldType = "text"
	FieldNumber   FieldType = "number"
	FieldBoolean  FieldType = "boolean"
	FieldDate     FieldType = "date"
	FieldDateTime FieldType = "datetime"
	FieldOption   FieldType = "option"
	FieldOptions  FieldType = "options"
	FieldAccount  FieldType = "account"
	FieldAccounts FieldType = "accounts"
	FieldLabels   FieldType = "labels"
	FieldRichText FieldType = "rich_text"
)

// FieldValue holds a native value with its declared type. JSON null clears a
// value. Omitting the field from a patch leaves it unchanged.
type FieldValue struct {
	Type  FieldType       `json:"type"`
	Value json.RawMessage `json:"value"`
}

type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type RichText struct {
	Format  string          `json:"format"`
	Content json.RawMessage `json:"content"`
}

type ScopeContext struct {
	AccountRef            string          `json:"account_ref"`
	EndpointRef           string          `json:"endpoint_ref"`
	ScopeRef              string          `json:"scope_ref"`
	EndpointConfig        json.RawMessage `json:"endpoint_config"`
	ScopeConfig           json.RawMessage `json:"scope_config"`
	EndpointSchemaVersion int             `json:"endpoint_schema_version"`
	ScopeSchemaVersion    int             `json:"scope_schema_version"`
}
