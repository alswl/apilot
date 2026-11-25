package server

import (
	"context"
	"encoding/json"
	"github.com/alswl/apilot/connectors/protocol"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

type workbenchConnectorConnectionID struct {
	ConnectionID string `path:"connection_id" minLength:"1"`
}

type workbenchConnectorSyncInput struct {
	ConnectionID string `path:"connection_id" minLength:"1"`
	Mode         string `query:"mode" enum:"auto,full" default:"auto"`
}

type workbenchConnectorOptionsInput struct {
	ID              string `path:"id" minLength:"1"`
	ScopeType       string `query:"scope_type"`
	ConfigLevel     string `query:"config_level" enum:"endpoint,scope"`
	EndpointConfig  string `query:"endpoint_config" maxLength:"4096"`
	ScopeConfig     string `query:"scope_config" maxLength:"4096"`
	AuthorizationID string `query:"authorization_id" required:"true" minLength:"1"`
	Field           string `query:"field" required:"true" minLength:"1" maxLength:"100"`
	Search          string `query:"search" maxLength:"200"`
	Page            string `query:"page" maxLength:"512"`
}

func registerWorkbenchConnectorHTTP(api huma.API, s *services.Service) {
	const auth = workbenchPath + "/connector-authorizations"
	const connections = workbenchPath + "/connector-connections"
	registerWorkbench(api, http.MethodPost, auth, "startWorkbenchConnectorAuthorization", "Start Connector browser login", "Connectors", []int{400, 404, 409},
		func(in *workbenchBody[common.WorkbenchConnectorLoginInput]) (common.WorkbenchConnectorAuthorization, error) {
			return s.StartWorkbenchConnectorAuthorization(in.Body)
		})
	registerWorkbench(api, http.MethodGet, auth+"/{id}", "getWorkbenchConnectorAuthorization", "Read Connector login status", "Connectors", []int{404},
		func(in *workbenchID) (common.WorkbenchConnectorAuthorization, error) {
			return s.WorkbenchConnectorAuthorization(in.ID)
		})
	registerWorkbench(api, http.MethodDelete, auth+"/{id}", "cancelWorkbenchConnectorAuthorization", "Cancel Connector browser login", "Connectors", []int{404, 409},
		func(in *workbenchID) (common.WorkbenchConnectorAuthorization, error) {
			return s.CancelWorkbenchConnectorAuthorization(in.ID)
		})
	registerWorkbenchContext(api, http.MethodGet, workbenchPath+"/connectors/{id}/options", "listWorkbenchConnectorOptions", "Read Connector configuration options", "Connectors", []int{400, 401, 403, 404, 409, 429, 503},
		func(ctx context.Context, in *workbenchConnectorOptionsInput) (common.WorkbenchConnectorOptions, error) {
			ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
			defer cancel()
			draft := protocol.OptionsParams{ConfigLevel: in.ConfigLevel}
			for _, target := range []struct {
				raw    string
				target *json.RawMessage
			}{
				{in.EndpointConfig, &draft.EndpointConfig}, {in.ScopeConfig, &draft.ScopeConfig},
			} {
				if target.raw == "" {
					continue
				}
				var object map[string]json.RawMessage
				if json.Unmarshal([]byte(target.raw), &object) != nil || object == nil || in.ConfigLevel == "" {
					return common.WorkbenchConnectorOptions{}, common.Err("INVALID_ARGUMENT", "Options configuration must be an object with a configuration level")
				}
				*target.target = json.RawMessage(target.raw)
			}
			return s.WorkbenchConnectorOptions(ctx, in.ID, in.AuthorizationID, in.Field, in.Search, in.Page, in.ScopeType, draft)
		})
	registerWorkbench(api, http.MethodGet, connections, "listWorkbenchConnectorConnections", "List Connector connections and their scopes", "Connections", nil,
		func(_ *struct{}) (workbenchItems[common.WorkbenchConnectorOverview], error) {
			items, err := s.WorkbenchConnectorConnections("")
			return workbenchItems[common.WorkbenchConnectorOverview]{items}, err
		})
	registerWorkbenchContext(api, http.MethodPut, connections, "saveWorkbenchConnectorConnection", "Save scopes for one authorized account", "Connections", []int{400, 404, 409, 503},
		func(ctx context.Context, in *workbenchBody[common.WorkbenchConnectorSaveInput]) (common.WorkbenchConnectorSaveResult, error) {
			ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
			defer cancel()
			return s.SaveWorkbenchConnectorConnection(ctx, in.Body)
		})
	registerWorkbench(api, http.MethodGet, connections+"/{connection_id}", "getWorkbenchConnectorConnection", "Read a Connector connection and its scope results", "Connections", []int{404},
		func(in *workbenchConnectorConnectionID) (common.WorkbenchConnectorOverview, error) {
			items, err := s.WorkbenchConnectorConnections(in.ConnectionID)
			if err != nil {
				return common.WorkbenchConnectorOverview{}, err
			}
			return items[0], nil
		})
	registerWorkbench(api, http.MethodDelete, connections+"/{connection_id}", "removeWorkbenchConnectorConnection", "Remove a Connector connection and its scopes", "Connections", []int{404, 409},
		func(in *workbenchConnectorConnectionID) (workbenchResultID, error) {
			return workbenchResultID{in.ConnectionID}, s.RemoveWorkbenchConnectorConnection(in.ConnectionID)
		})
	registerWorkbench(api, http.MethodPost, connections+"/{connection_id}/sync", "syncWorkbenchConnectorConnection", "Run sync for the scopes of one connection", "Sync", []int{404, 409},
		func(in *workbenchConnectorSyncInput) (workbenchItems[common.WorkbenchConnectorScopeResult], error) {
			items, err := s.StartWorkbenchConnectorSync(in.ConnectionID, in.Mode == "full")
			return workbenchItems[common.WorkbenchConnectorScopeResult]{items}, err
		})
}
