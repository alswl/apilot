package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func connectorBoundary(scope common.WorkbenchConnectorStoredScope, schema protocol.ConfigSchema) common.WorkbenchConnectorBoundary {
	config := make(map[string]any)
	var saved map[string]json.RawMessage
	if json.Unmarshal(scope.Context.ScopeConfig, &saved) == nil {
		for key, raw := range saved {
			if connectorConfigAllowed(schema, json.RawMessage(encodeScopeField(key, raw))) {
				config[key] = raw
			}
		}
	}
	return common.WorkbenchConnectorBoundary{ScopeType: scope.ScopeType, ScopeRef: scope.ScopeRef, Label: scope.Config.Name,
		Config: config, SchemaVersion: scope.Context.ScopeSchemaVersion}
}

func encodeScopeField(key string, raw json.RawMessage) []byte {
	object, _ := json.Marshal(map[string]json.RawMessage{key: raw})
	return object
}

func (s *Service) editableConnectorScope(id string) (common.WorkbenchConnectorStoredScope, *managedConnectorRelease, error) {
	store, err := s.managedConnectorStore()
	if err != nil {
		return common.WorkbenchConnectorStoredScope{}, nil, err
	}
	scope, err := store.WorkbenchConnectorScope(id)
	if err != nil {
		return scope, nil, err
	}
	process, err := s.managedConnector(scope.ConnectorID)
	if err != nil {
		return scope, nil, err
	}
	connector := s.registeredConnector(scope.ConnectorID)
	if connector == nil || connector.process != process {
		return scope, nil, common.Err("STALE_REVISION", "Connector release changed")
	}
	return scope, connector, nil
}

func (s *Service) WorkbenchConnectorScopeEdit(id string) (common.WorkbenchConnectorScopeEdit, error) {
	scope, connector, err := s.editableConnectorScope(id)
	if err != nil {
		return common.WorkbenchConnectorScopeEdit{}, err
	}
	schema := connector.description.ScopeConfig
	return common.WorkbenchConnectorScopeEdit{ScopeID: id, Revision: scope.ConfigRevision, Boundary: connectorBoundary(scope, schema), ConfigSchema: schema}, nil
}

func (s *Service) scopeEditCurrent(scope common.WorkbenchConnectorStoredScope, connector *managedConnectorRelease) error {
	current, active, err := s.editableConnectorScope(scope.ScopeID)
	if err != nil {
		return err
	}
	if active != connector || current.AuthGeneration != scope.AuthGeneration || current.ConfigRevision != scope.ConfigRevision {
		return common.Err("STALE_REVISION", "Connector scope changed")
	}
	return nil
}

func (s *Service) WorkbenchConnectorScopeOptions(ctx context.Context, id string, revision int64, options protocol.OptionsParams) (common.WorkbenchConnectorOptions, error) {
	var out common.WorkbenchConnectorOptions
	scope, connector, err := s.editableConnectorScope(id)
	if err != nil {
		return out, err
	}
	schema := connector.description.ScopeConfig
	if scope.ConfigRevision != revision {
		return out, common.Err("STALE_REVISION", "Connector scope changed")
	}
	if scope.AuthState != "authorized" {
		return out, common.Err("UNAUTHORIZED", "Connector authorization is required")
	}
	if !validScopeEditType(schema, options.ScopeType) || !connectorConfigAllowed(schema, options.ScopeConfig) ||
		(schema.Fields[options.Field].Control == "password" || schema.Fields[options.Field].Control == "secret") {
		return out, common.Err("INVALID_ARGUMENT", "Invalid scope option context")
	}
	options.AccountRef, options.EndpointRef = scope.Context.AccountRef, scope.Context.EndpointRef
	if connector.trusted {
		options.ConfigLevel = "scope"
		options.EndpointConfig, options.EndpointSchemaVersion = scope.Context.EndpointConfig, scope.Context.EndpointSchemaVersion
	} else if len(options.ScopeConfig) != 0 {
		return out, common.Err("INVALID_ARGUMENT", "Unsupported configuration options context")
	}
	out.ConfigOptions, err = callWorkbenchConnector[protocol.ConfigOptions](ctx, connector.process, common.ID(), protocol.Options, options, nil)
	if err != nil {
		return out, publicConnectorError(err)
	}
	if err := s.scopeEditCurrent(scope, connector); err != nil {
		return common.WorkbenchConnectorOptions{}, err
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return out, err
	}
	connected, err := store.WorkbenchConnectorSelectedValues(scope.ConnectorID, options.EndpointRef, options.AccountRef, options.Field, options.ScopeType, id)
	if err != nil {
		return out, err
	}
	var selected string
	if options.ScopeType == scope.ScopeType {
		raw, _ := connectorBoundary(scope, schema).Config[options.Field].(json.RawMessage)
		_ = json.Unmarshal(raw, &selected)
	}
	if selected != "" && options.Page == "" {
		option := protocol.ConfigOption{Value: selected, Label: selected}
		if i := slices.IndexFunc(out.Items, func(item protocol.ConfigOption) bool { return item.Value == selected }); i >= 0 {
			option = out.Items[i]
		}
		out.SelectedOption = &option
	}
	for _, item := range out.Items {
		if connected[item.Value] {
			out.ConnectedValues = append(out.ConnectedValues, item.Value)
		}
	}
	return out, nil
}

func validScopeEditType(schema protocol.ConfigSchema, kind string) bool {
	return kind == "" && len(schema.Types) == 0 || slices.Contains(schema.Types, kind)
}

func (s *Service) validateScopeEdit(ctx context.Context, scope common.WorkbenchConnectorStoredScope, connector *managedConnectorRelease, draft common.WorkbenchConnectorScopeDraft) (common.WorkbenchConnectorScopePreview, error) {
	var out common.WorkbenchConnectorScopePreview
	schema := connector.description.ScopeConfig
	if draft.Revision != scope.ConfigRevision || draft.SchemaVersion != schema.Version {
		return out, common.Err("STALE_REVISION", "Connector scope or schema changed")
	}
	if scope.AuthState != "authorized" {
		return out, common.Err("UNAUTHORIZED", "Connector authorization is required")
	}
	raw, err := json.Marshal(draft.Config)
	if err != nil || draft.Config == nil || !validScopeEditType(schema, draft.ScopeType) ||
		!common.ConnectorConfigAllowsOrdinaryValues(&schema, draft.Config) {
		return out, common.Err("INVALID_ARGUMENT", "Invalid Connector scope configuration")
	}
	params := scope.Context
	params.ScopeRef, params.ScopeType, params.ScopeConfig, params.ScopeSchemaVersion = "", draft.ScopeType, raw, draft.SchemaVersion
	validated, err := validateConnectorScope(ctx, connector.process, params, "static")
	if err != nil {
		return out, publicConnectorError(err)
	}
	params.ScopeRef = validated.ScopeRef
	validated, err = validateConnectorScope(ctx, connector.process, params, "access")
	if err != nil {
		return out, publicConnectorError(err)
	}
	out.Before = connectorBoundary(scope, schema)
	out.After = common.WorkbenchConnectorBoundary{ScopeType: draft.ScopeType, ScopeRef: validated.ScopeRef, Label: validated.Label, Config: draft.Config, SchemaVersion: draft.SchemaVersion}
	if unchangedScopeBoundary(scope, out.After, raw) {
		out.After.Label = out.Before.Label
	}
	binding, err := json.Marshal(struct {
		ID, Release    string
		Revision, Auth int64
		After          common.WorkbenchConnectorBoundary
	}{scope.ScopeID, connector.release.PackageDigest, scope.ConfigRevision, scope.AuthGeneration, out.After})
	if err != nil {
		return common.WorkbenchConnectorScopePreview{}, err
	}
	hash := sha256.Sum256(binding)
	out.PreviewToken = hex.EncodeToString(hash[:])
	return out, nil
}

func validateConnectorScope(ctx context.Context, process *integrations.WorkbenchConnectorProcess, params protocol.ScopeContext, mode string) (protocol.ValidatedScope, error) {
	result, err := callWorkbenchConnector[protocol.ValidatedScope](ctx, process, common.ID(), protocol.Validate, protocol.ValidateParams{ScopeContext: params, Mode: mode}, nil)
	if err == nil && (result.ScopeRef == "" || result.EndpointRef != params.EndpointRef || result.Label == "" ||
		(mode == "access" && result.ScopeRef != params.ScopeRef)) {
		err = integrations.ErrWorkbenchConnectorProtocol
	}
	return result, err
}

func (s *Service) PreviewWorkbenchConnectorScope(ctx context.Context, id string, draft common.WorkbenchConnectorScopeDraft) (common.WorkbenchConnectorScopePreview, error) {
	scope, connector, err := s.editableConnectorScope(id)
	if err != nil {
		return common.WorkbenchConnectorScopePreview{}, err
	}
	out, err := s.validateScopeEdit(ctx, scope, connector, draft)
	if err != nil {
		return out, err
	}
	if err := s.scopeEditCurrent(scope, connector); err != nil {
		return common.WorkbenchConnectorScopePreview{}, err
	}
	return out, nil
}

func (s *Service) UpdateWorkbenchConnectorScope(ctx context.Context, id string, input common.WorkbenchConnectorScopeUpdate) (common.WorkbenchConnectorScopeUpdateResult, error) {
	var out common.WorkbenchConnectorScopeUpdateResult
	scope, connector, err := s.editableConnectorScope(id)
	if err != nil {
		return out, err
	}
	preview, err := s.validateScopeEdit(ctx, scope, connector, input.WorkbenchConnectorScopeDraft)
	if err != nil {
		return out, err
	}
	if preview.PreviewToken != input.PreviewToken {
		return out, common.Err("STALE_REVISION", "Connector scope preview changed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if s.stopping || s.workbenchActive[id] != nil {
		return out, common.Err("CONFLICT", "Wait for the active scope sync before saving")
	}
	if err := s.scopeEditCurrent(scope, connector); err != nil {
		return out, err
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(preview.After.Config)
	out.WorkbenchConnectorScopeEdit = common.WorkbenchConnectorScopeEdit{ScopeID: id, Revision: scope.ConfigRevision, Boundary: preview.After, ConfigSchema: connector.description.ScopeConfig}
	if unchangedScopeBoundary(scope, preview.After, raw) {
		out.Boundary = preview.Before
		out.SyncStatus = "unchanged"
		return out, nil
	}
	if err := store.UpdateWorkbenchConnectorScope(scope, preview.After, raw); err != nil {
		return common.WorkbenchConnectorScopeUpdateResult{}, err
	}
	out.Revision++
	out.SyncStatus = "queued"
	if _, err := s.startWorkbenchConnectorRunLocked(store, id, "manual", true); err != nil {
		out.SyncStatus = "sync_not_started"
	}
	s.volatileRevision.Add(1)
	return out, nil
}

func unchangedScopeBoundary(scope common.WorkbenchConnectorStoredScope, boundary common.WorkbenchConnectorBoundary, config []byte) bool {
	return scope.ScopeRef == boundary.ScopeRef && scope.ScopeType == boundary.ScopeType &&
		scope.Context.ScopeSchemaVersion == boundary.SchemaVersion && strings.TrimSpace(string(scope.Context.ScopeConfig)) == string(config)
}
