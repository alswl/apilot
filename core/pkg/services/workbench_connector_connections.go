package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

const connectorAuthLifetime = 10 * time.Minute

type connectorAuthorization struct {
	status   common.WorkbenchConnectorAuthorization
	account  protocol.Account
	expected common.WorkbenchConnectorConnection
	cancel   context.CancelFunc
}

type workbenchManagedConnectorStore interface {
	SaveWorkbenchConnectorConnection(common.WorkbenchConnectorConnection, []common.WorkbenchConnectorNewScope) (common.WorkbenchConnectorSaveResult, error)
	WorkbenchConnectorConnection(string) (common.WorkbenchConnectorConnection, error)
	WorkbenchConnectorSelectedValues(string, string, string, string) (map[string]bool, error)
	ConfirmWorkbenchConnectorAccount(string, int64) error
	WorkbenchConnectorScope(string) (common.WorkbenchConnectorStoredScope, error)
	WorkbenchConnectorOverviews(string) ([]common.WorkbenchConnectorOverview, error)
	DeleteWorkbenchConnectorConnection(string) error
	RecoverWorkbenchConnectorRuns() error
	StartWorkbenchConnectorRun(common.WorkbenchSyncRun) (int64, error)
	WorkbenchConnectorLastRun(string) (*common.WorkbenchSyncRun, error)
	WorkbenchConnectorBackoff(string, string, []string) (*time.Time, error)
	PauseWorkbenchConnectorScope(string, int64, bool) error
	FinishWorkbenchConnectorFailure(common.WorkbenchConnectorSyncCommit) error
	CommitWorkbenchConnectorSync(common.WorkbenchConnectorSyncCommit) error
	WorkbenchConnectorMembers(string) ([]common.WorkbenchTask, error)
}

func (s *Service) managedConnectorStore() (workbenchManagedConnectorStore, error) {
	store, ok := s.store.(workbenchManagedConnectorStore)
	if !ok {
		return nil, common.Err("INTERNAL", "Connector storage unavailable")
	}
	return store, nil
}

func (s *Service) managedConnector(id string) (*integrations.WorkbenchConnectorProcess, error) {
	if s.workbenchConnectorRelease == nil || s.workbenchConnectorRelease.ConnectorID != id {
		return nil, common.Err("NOT_FOUND", "Connector release unavailable")
	}
	if err := s.workbenchPluginAvailable(s.managedPluginID(id)); err != nil {
		return nil, err
	}
	return s.workbenchConnectorProcess, nil
}

func (s *Service) managedPluginID(id string) string {
	if id == "github-issues" {
		return common.WorkbenchGitHubRepository
	}
	return id
}

func (s *Service) StartWorkbenchConnectorAuthorization(in common.WorkbenchConnectorLoginInput) (common.WorkbenchConnectorAuthorization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty common.WorkbenchConnectorAuthorization
	if s.stopping {
		return empty, common.Err("CONFLICT", "App is stopping")
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return empty, err
	}
	var expected common.WorkbenchConnectorConnection
	if in.ConnectionID != "" {
		expected, err = store.WorkbenchConnectorConnection(in.ConnectionID)
		if err != nil {
			return empty, err
		}
		if expected.ConnectorID != in.ConnectorID {
			return empty, common.Err("INVALID_ARGUMENT", "Connector does not match the connection")
		}
	}
	process, err := s.managedConnector(in.ConnectorID)
	if err != nil {
		return empty, err
	}
	now := time.Now().UTC()
	if s.workbenchConnectorAuth == nil {
		s.workbenchConnectorAuth = make(map[string]*connectorAuthorization)
	}
	for id, session := range s.workbenchConnectorAuth {
		if now.After(session.status.ExpiresAt.Add(time.Minute)) {
			delete(s.workbenchConnectorAuth, id)
		}
	}
	if len(s.workbenchConnectorAuth) >= 32 {
		return empty, common.Err("CONFLICT", "Too many Connector authorization sessions")
	}
	ctx, cancel := context.WithDeadline(s.ctx, now.Add(connectorAuthLifetime))
	session := &connectorAuthorization{
		status:   common.WorkbenchConnectorAuthorization{ID: common.ID(), ConnectorID: in.ConnectorID, Status: "waiting", ExpiresAt: now.Add(connectorAuthLifetime)},
		expected: expected, cancel: cancel,
	}
	s.workbenchConnectorAuth[session.status.ID] = session
	s.wg.Add(1)
	go s.runWorkbenchConnectorAuthorization(ctx, process, store, session)
	return session.status, nil
}

func (s *Service) runWorkbenchConnectorAuthorization(ctx context.Context, process *integrations.WorkbenchConnectorProcess, store workbenchManagedConnectorStore, session *connectorAuthorization) {
	defer s.wg.Done()
	defer session.cancel()
	id := session.status.ID
	account, err := callWorkbenchConnector[protocol.Account](ctx, process, id, protocol.Login,
		protocol.LoginParams{AccountRef: session.expected.AccountRef, EndpointRef: session.expected.EndpointRef},
		func(msg protocol.Message) error {
			if msg.Method != "open_browser" {
				return nil
			}
			var event protocol.BrowserNotification
			if err := json.Unmarshal(msg.Params, &event); err != nil {
				return err
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if session.status.Status == "waiting" {
				session.status.VerificationURL, session.status.UserCode = event.URL, event.UserCode
			}
			return nil
		})
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.status.Status != "waiting" {
		return
	}
	session.status.VerificationURL, session.status.UserCode = "", ""
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && (account.AccountRef == "" || account.EndpointRef == "" || account.DisplayName == "" ||
		(session.expected.ID != "" && (account.AccountRef != session.expected.AccountRef || account.EndpointRef != session.expected.EndpointRef))) {
		err = protocol.Problem(protocol.AccessDenied, "account", "The original account is required")
	}
	if err == nil && session.expected.ID != "" {
		err = store.ConfirmWorkbenchConnectorAccount(session.expected.ID, session.expected.AuthGeneration)
	}
	if err != nil {
		session.status.Status = "failed"
		session.status.ErrorKind = connectorErrorKind(err)
		var problem *protocol.RPCError
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &problem) && problem.Data.Kind == protocol.AuthRequired && problem.Data.Reason == protocol.AuthorizationExpired) {
			session.status.Status = "expired"
		}
		if errors.Is(err, context.Canceled) {
			session.status.Status = "cancelled"
		}
		return
	}
	session.account = account
	session.status.Status, session.status.DisplayName, session.status.EndpointRef = "authorized", account.DisplayName, account.EndpointRef
	if session.expected.ID != "" {
		if rows, err := store.WorkbenchConnectorOverviews(session.expected.ID); err == nil && len(rows) == 1 {
			for _, scope := range rows[0].Scopes {
				if s.workbenchActive[scope.ScopeID] == nil {
					_, _ = s.startWorkbenchConnectorRunLocked(store, scope.ScopeID, "relogin", true)
				}
			}
		}
	}
}

func (s *Service) WorkbenchConnectorAuthorization(id string) (common.WorkbenchConnectorAuthorization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.workbenchConnectorAuth[id]
	if session == nil {
		return common.WorkbenchConnectorAuthorization{}, common.Err("NOT_FOUND", "Unknown Connector authorization")
	}
	status := session.status
	if status.Status == "authorized" && time.Now().After(status.ExpiresAt) {
		status.Status = "expired"
	}
	return status, nil
}

func (s *Service) CancelWorkbenchConnectorAuthorization(id string) (common.WorkbenchConnectorAuthorization, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.workbenchConnectorAuth[id]
	if session == nil {
		return common.WorkbenchConnectorAuthorization{}, common.Err("NOT_FOUND", "Unknown Connector authorization")
	}
	switch session.status.Status {
	case "waiting":
		session.status.Status = "cancelled"
		session.status.VerificationURL, session.status.UserCode = "", ""
		session.cancel()
	case "cancelled":
	default:
		return common.WorkbenchConnectorAuthorization{}, common.Err("CONFLICT", "Authorization already completed")
	}
	return session.status, nil
}

func (s *Service) authorizedConnectorAccount(id, connectorID string) (*integrations.WorkbenchConnectorProcess, protocol.Account, error) {
	session := s.workbenchConnectorAuth[id]
	if session == nil || (connectorID != "" && connectorID != session.status.ConnectorID) {
		return nil, protocol.Account{}, common.Err("NOT_FOUND", "Unknown Connector authorization")
	}
	if session.status.Status != "authorized" || time.Now().After(session.status.ExpiresAt) {
		return nil, protocol.Account{}, common.Err("CONFLICT", "Complete Connector authorization before choosing scopes")
	}
	if s.stopping {
		return nil, protocol.Account{}, common.Err("CONFLICT", "App is stopping")
	}
	process, err := s.managedConnector(session.status.ConnectorID)
	return process, session.account, err
}

func (s *Service) WorkbenchConnectorOptions(ctx context.Context, id, authorizationID, field, search, page string) (common.WorkbenchConnectorOptions, error) {
	var out common.WorkbenchConnectorOptions
	s.mu.Lock()
	process, account, err := s.authorizedConnectorAccount(authorizationID, id)
	s.mu.Unlock()
	if err != nil {
		return out, err
	}
	out.ConfigOptions, err = callWorkbenchConnector[protocol.ConfigOptions](ctx, process, common.ID(), protocol.Options, protocol.OptionsParams{
		AccountRef: account.AccountRef, EndpointRef: account.EndpointRef, Field: field, Search: search, Page: page,
	}, nil)
	if err != nil {
		return out, publicConnectorError(err)
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return out, err
	}
	connected, err := store.WorkbenchConnectorSelectedValues(id, account.EndpointRef, account.AccountRef, field)
	if err != nil {
		return out, err
	}
	for _, item := range out.Items {
		if connected[item.Value] {
			out.ConnectedValues = append(out.ConnectedValues, item.Value)
		}
	}
	return out, nil
}

func (s *Service) SaveWorkbenchConnectorConnection(ctx context.Context, in common.WorkbenchConnectorSaveInput) (common.WorkbenchConnectorSaveResult, error) {
	var empty common.WorkbenchConnectorSaveResult
	if len(in.Scopes) == 0 || len(in.Scopes) > 20 || in.EndpointSchemaVersion <= 0 || in.ScopeSchemaVersion <= 0 {
		return empty, common.Err("INVALID_ARGUMENT", "Select between one and twenty scopes with valid schema versions")
	}
	s.mu.Lock()
	process, account, err := s.authorizedConnectorAccount(in.AuthorizationID, "")
	if err != nil {
		s.mu.Unlock()
		return empty, err
	}
	id := s.workbenchConnectorAuth[in.AuthorizationID].status.ConnectorID
	if id != "github-issues" && id != "fixture" {
		for i := range in.Scopes {
			in.Scopes[i].WriteEnabled = false
		}
	}
	s.mu.Unlock()
	endpoint := in.EndpointConfig
	if endpoint == nil {
		endpoint = map[string]any{}
	}
	rawEndpoint, err := json.Marshal(endpoint)
	if err != nil {
		return empty, common.Err("INVALID_ARGUMENT", "Invalid endpoint configuration")
	}
	var selections []common.WorkbenchConnectorNewScope
	out := common.WorkbenchConnectorSaveResult{Scopes: make([]common.WorkbenchConnectorScopeResult, len(in.Scopes))}
	var indices []int
	seen := make(map[string]int)
	repeated := make(map[int]int)
	for i, selection := range in.Scopes {
		rawScope, err := json.Marshal(selection.Config)
		if selection.Config == nil || err != nil {
			out.Scopes[i] = common.WorkbenchConnectorScopeResult{Status: "rejected", ErrorKind: string(protocol.InvalidConfig)}
			continue
		}
		params := protocol.ScopeContext{AccountRef: account.AccountRef, EndpointRef: account.EndpointRef, EndpointConfig: rawEndpoint, ScopeConfig: rawScope,
			EndpointSchemaVersion: in.EndpointSchemaVersion, ScopeSchemaVersion: in.ScopeSchemaVersion}
		scope, err := callWorkbenchConnector[protocol.ValidatedScope](ctx, process, common.ID(), protocol.Validate, protocol.ValidateParams{ScopeContext: params, Mode: "static"}, nil)
		if err == nil && (scope.ScopeRef == "" || scope.EndpointRef != account.EndpointRef || scope.Label == "") {
			err = integrations.ErrWorkbenchConnectorProtocol
		}
		if err == nil {
			if first, ok := seen[scope.ScopeRef]; ok {
				repeated[i] = first
				continue
			}
			seen[scope.ScopeRef] = i
			params.ScopeRef = scope.ScopeRef
			var access protocol.ValidatedScope
			access, err = callWorkbenchConnector[protocol.ValidatedScope](ctx, process, common.ID(), protocol.Validate, protocol.ValidateParams{ScopeContext: params, Mode: "access"}, nil)
			if err == nil && (access.ScopeRef != scope.ScopeRef || access.EndpointRef != account.EndpointRef || access.Label == "") {
				err = integrations.ErrWorkbenchConnectorProtocol
			}
			if err == nil {
				scope = access
			}
		}
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		if err != nil {
			out.Scopes[i] = common.WorkbenchConnectorScopeResult{ScopeRef: scope.ScopeRef, Label: scope.Label, Status: "rejected", ErrorKind: connectorErrorKind(err)}
			continue
		}
		indices = append(indices, i)
		selections = append(selections, common.WorkbenchConnectorNewScope{ID: common.ID(), Context: params, Label: scope.Label, DisplayName: account.DisplayName, WriteEnabled: selection.WriteEnabled})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, _, err := s.authorizedConnectorAccount(in.AuthorizationID, id); err != nil {
		return empty, err
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if len(selections) > 0 {
		store, err := s.managedConnectorStore()
		if err != nil {
			return empty, err
		}
		result, err := store.SaveWorkbenchConnectorConnection(common.WorkbenchConnectorConnection{ID: common.ID(), ConnectorID: id, EndpointRef: account.EndpointRef, AccountRef: account.AccountRef}, selections)
		if err != nil {
			return empty, err
		}
		out.ConnectionID = result.ConnectionID
		for j, scope := range result.Scopes {
			if scope.Status == "queued" {
				if _, err := s.startWorkbenchConnectorRunLocked(store, scope.ScopeID, "initial", true); err != nil {
					scope.Status, scope.ErrorKind = "sync_not_started", connectorErrorKind(err)
				}
			}
			out.Scopes[indices[j]] = scope
		}
	}
	for i, prior := range repeated {
		out.Scopes[i] = out.Scopes[prior]
		out.Scopes[i].Status = "duplicate_selection"
	}
	s.volatileRevision.Add(1)
	return out, nil
}

func (s *Service) WorkbenchConnectorConnections(id string) ([]common.WorkbenchConnectorOverview, error) {
	store, err := s.managedConnectorStore()
	if err != nil {
		return nil, err
	}
	items, err := store.WorkbenchConnectorOverviews(id)
	if err == nil && id != "" && len(items) == 0 {
		err = common.Err("NOT_FOUND", "Unknown Connector connection")
	}
	return items, err
}

func (s *Service) RemoveWorkbenchConnectorConnection(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return common.Err("CONFLICT", "App is stopping")
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return err
	}
	items, err := store.WorkbenchConnectorOverviews(id)
	if err != nil {
		return err
	}
	if len(items) != 1 {
		return common.Err("NOT_FOUND", "Unknown Connector connection")
	}
	if err := store.DeleteWorkbenchConnectorConnection(id); err != nil {
		return err
	}
	for _, scope := range items[0].Scopes {
		if cancel := s.workbenchActive[scope.ScopeID]; cancel != nil {
			cancel()
		}
	}
	s.volatileRevision.Add(1)
	return nil
}

func callWorkbenchConnector[T any](ctx context.Context, process *integrations.WorkbenchConnectorProcess, id, method string, params any, notify func(protocol.Message) error) (T, error) {
	var result T
	raw, err := json.Marshal(params)
	if err != nil {
		return result, integrations.ErrWorkbenchConnectorProtocol
	}
	response, err := process.Call(ctx, protocol.Request{ID: id, Method: method, Params: raw}, notify)
	if err != nil {
		return result, err
	}
	if len(response) == 0 || response[0] != '{' || json.Unmarshal(response, &result) != nil {
		return result, integrations.ErrWorkbenchConnectorProtocol
	}
	return result, nil
}

func connectorErrorKind(err error) string {
	var problem *protocol.RPCError
	switch {
	case errors.As(err, &problem):
		switch problem.Data.Kind {
		case protocol.AuthRequired, protocol.AccessDenied, protocol.InvalidConfig, protocol.InvalidCheckpoint, protocol.RateLimited, protocol.Temporary, protocol.Unsupported:
			return string(problem.Data.Kind)
		}
		return common.WorkbenchConnectorProtocolFailure
	case errors.Is(err, context.Canceled):
		return common.WorkbenchConnectorCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return common.WorkbenchConnectorTimeout
	case errors.Is(err, integrations.ErrWorkbenchConnectorProtocol):
		return common.WorkbenchConnectorProtocolFailure
	default:
		return common.WorkbenchConnectorProcessFailure
	}
}

func publicConnectorError(err error) error {
	if err == nil {
		return nil
	}
	switch connectorErrorKind(err) {
	case string(protocol.InvalidConfig):
		return common.Err("INVALID_ARGUMENT", "Connector configuration is invalid")
	case string(protocol.AuthRequired):
		return common.Err("UNAUTHORIZED", "Connector authorization is required")
	case string(protocol.AccessDenied):
		return common.Err("FORBIDDEN", "Connector access was denied")
	case string(protocol.RateLimited):
		return common.Err("RATE_LIMITED", "Connector rate limit reached")
	default:
		return common.Err("SOURCE_UNAVAILABLE", "Connector request could not finish")
	}
}

func (s *Service) ConfigureWorkbenchConnectorScope(id, kind, name string, version int64, write bool) (common.WorkbenchSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty common.WorkbenchSnapshot
	if s.stopping {
		return empty, common.Err("CONFLICT", "App is stopping")
	}
	name = strings.TrimSpace(name)
	if version < 1 || name == "" || len(name) > 256 {
		return empty, common.Err("INVALID_ARGUMENT", "Scope name and version are required")
	}
	scope, err := s.WorkbenchSnapshot(id)
	if err != nil {
		return empty, err
	}
	if scope.ParentConnectionID == "" || scope.Kind != kind {
		return empty, common.Err("INVALID_ARGUMENT", "Connector type cannot change")
	}
	if kind != "github-issues" && kind != "fixture" && write {
		return empty, common.Err("FORBIDDEN", "Connector is read-only")
	}
	if scope.Version != version {
		return empty, common.Err("STALE_REVISION", "Scope configuration changed")
	}
	if scope.Name == name && scope.WriteEnabled == write {
		return scope, nil
	}
	store, ok := s.store.(interface {
		ConfigureWorkbenchConnectorScope(string, string, int64, bool) error
	})
	if !ok {
		return empty, common.Err("INTERNAL", "Scope configuration storage unavailable")
	}
	if err := store.ConfigureWorkbenchConnectorScope(id, name, version, write); err != nil {
		return empty, err
	}
	s.volatileRevision.Add(1)
	return s.WorkbenchSnapshot(id)
}
