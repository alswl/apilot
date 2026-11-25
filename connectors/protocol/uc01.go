package protocol

import (
	"context"
	"encoding/json"
	"time"
)

const (
	Login    = "auth.login"
	Options  = "config.options"
	Validate = "connection.validate"
	Sync     = "tasks.sync"
	Resolve  = "tasks.resolve"
)

type LoginParams struct {
	AccountRef  string `json:"account_ref,omitempty"`
	EndpointRef string `json:"endpoint_ref,omitempty"`
}

type Account struct {
	AccountRef  string `json:"account_ref"`
	EndpointRef string `json:"endpoint_ref"`
	DisplayName string `json:"display_name"`
}

type OptionsParams struct {
	ScopeType   string `json:"scope_type,omitempty"`
	AccountRef  string `json:"account_ref"`
	EndpointRef string `json:"endpoint_ref"`
	Field       string `json:"field"`
	Search      string `json:"search,omitempty"`
	Page        string `json:"page,omitempty"`
}

type ConfigOptions struct {
	Items    []ConfigOption `json:"items"`
	NextPage string         `json:"next_page,omitempty"`
}

type ConfigOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type ValidateParams struct {
	ScopeContext
	Mode string `json:"mode"`
}

type ValidatedScope struct {
	EndpointRef string `json:"endpoint_ref"`
	ScopeRef    string `json:"scope_ref"`
	Label       string `json:"label"`
}

type SyncParams struct {
	ScopeContext
	Mode       string   `json:"mode"`
	Fields     []string `json:"fields,omitempty"`
	Checkpoint string   `json:"checkpoint,omitempty"`
	Page       string   `json:"page,omitempty"`
}

type SourceRecord struct {
	SourceID      string                `json:"source_id"`
	SourceVersion string                `json:"source_version"`
	Number        string                `json:"number"`
	URL           string                `json:"url"`
	Title         string                `json:"title"`
	State         string                `json:"state"`
	StateID       string                `json:"state_id"`
	StateLabel    string                `json:"state_label"`
	UpdatedAt     time.Time             `json:"updated_at"`
	Fields        map[string]FieldValue `json:"fields,omitempty"`
}

type SyncPage struct {
	Records        []SourceRecord `json:"records"`
	Removals       []Resolution   `json:"removals,omitempty"`
	NextPage       string         `json:"next_page,omitempty"`
	Coverage       string         `json:"coverage,omitempty"`
	NextCheckpoint string         `json:"next_checkpoint,omitempty"`
	Usage          Usage          `json:"usage"`
}

type ResolveParams struct {
	ScopeContext
	KnownNumbers map[string]string `json:"known_numbers,omitempty"`
	SourceIDs    []string          `json:"source_ids"`
	Fields       []string          `json:"fields,omitempty"`
}

type Resolution struct {
	SourceID string        `json:"source_id"`
	Status   string        `json:"status"`
	Record   *SourceRecord `json:"record,omitempty"`
}

type ResolveResult struct {
	Items []Resolution `json:"items"`
	Usage Usage        `json:"usage"`
}

func Problem(kind ErrorKind, level, message string) *RPCError {
	return &RPCError{Code: -32000, Message: message, Data: ErrorData{Kind: kind, AffectedLevel: level}}
}

func DecodeParams(req Request, dest any) *RPCError {
	if len(req.Params) == 0 || req.Params[0] != '{' || json.Unmarshal(req.Params, dest) != nil {
		return Problem(InvalidConfig, "", "Invalid method parameters")
	}
	return nil
}

const AuthorizationExpired = "authorization_expired"

func ExpiredAuthorization(message string) *RPCError {
	problem := Problem(AuthRequired, "account", message)
	problem.Data.Reason = AuthorizationExpired
	return problem
}

func RequestContext(deadline string) (context.Context, context.CancelFunc) {
	if at, err := time.Parse(time.RFC3339Nano, deadline); err == nil {
		return context.WithDeadline(context.Background(), at)
	}
	return context.WithCancel(context.Background())
}
