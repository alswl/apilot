package dal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

type connectorScopeConfig struct {
	Mapping map[string]protocol.MappingRule `json:"mapping,omitempty"`
	common.WorkbenchConnection
	EndpointConfig        json.RawMessage `json:"endpoint_config"`
	ScopeConfig           json.RawMessage `json:"scope_config"`
	EndpointSchemaVersion int             `json:"endpoint_schema_version"`
	ScopeSchemaVersion    int             `json:"scope_schema_version"`
}

func (s *Store) SaveWorkbenchConnectorConnection(parent common.WorkbenchConnectorConnection, scopes []common.WorkbenchConnectorNewScope) (common.WorkbenchConnectorSaveResult, error) {
	out := common.WorkbenchConnectorSaveResult{Scopes: []common.WorkbenchConnectorScopeResult{}}
	tx, err := s.db.Begin()
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO workbench_connector_connections(id,connector_id,endpoint_ref,account_ref,auth_state)
		VALUES(?,?,?,?,'authorized') ON CONFLICT(connector_id,endpoint_ref,account_ref) DO NOTHING`, parent.ID, parent.ConnectorID, parent.EndpointRef, parent.AccountRef); err != nil {
		return out, err
	}
	if err := tx.QueryRow(`SELECT id FROM workbench_connector_connections WHERE connector_id=? AND endpoint_ref=? AND account_ref=?`,
		parent.ConnectorID, parent.EndpointRef, parent.AccountRef).Scan(&out.ConnectionID); err != nil {
		return out, err
	}
	for _, scope := range scopes {
		result := common.WorkbenchConnectorScopeResult{ScopeID: scope.ID, ScopeRef: scope.Context.ScopeRef, Label: scope.Label, Status: "queued"}
		if out.ConnectionID != parent.ID {
			result.ScopeID = ""
			err := tx.QueryRow("SELECT id FROM workbench_connections WHERE connector_connection_id=? AND scope_ref=?", out.ConnectionID, result.ScopeRef).Scan(&result.ScopeID)
			switch {
			case errors.Is(err, sql.ErrNoRows):
				result.Status, result.ErrorKind = "rejected", "connection_exists"
			case err != nil:
				return out, err
			default:
				result.Status = "already_connected"
			}
		} else {
			config := connectorScopeConfig{
				WorkbenchConnection: common.WorkbenchConnection{ID: scope.ID, Kind: parent.ConnectorID, Name: scope.Label, Mode: "sync", Version: 1, AccountLogin: scope.DisplayName, WriteEnabled: scope.WriteEnabled},
				EndpointConfig:      scope.Context.EndpointConfig, ScopeConfig: scope.Context.ScopeConfig,
				EndpointSchemaVersion: scope.Context.EndpointSchemaVersion, ScopeSchemaVersion: scope.Context.ScopeSchemaVersion,
			}
			_, err := tx.Exec(`INSERT INTO workbench_connections(id,config,version,connector_connection_id,scope_ref,config_revision)
				VALUES(?,?,1,?,?,1)`, scope.ID, encode(config), out.ConnectionID, scope.Context.ScopeRef)
			if err != nil {
				return out, err
			}
		}
		out.Scopes = append(out.Scopes, result)
	}
	return out, tx.Commit()
}

func (s *Store) WorkbenchConnectorConnection(id string) (common.WorkbenchConnectorConnection, error) {
	var parent common.WorkbenchConnectorConnection
	err := s.db.QueryRow(`SELECT id,connector_id,endpoint_ref,account_ref,auth_state,auth_generation
		FROM workbench_connector_connections WHERE id=?`, id).Scan(&parent.ID, &parent.ConnectorID, &parent.EndpointRef, &parent.AccountRef, &parent.AuthState, &parent.AuthGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		err = common.Err("NOT_FOUND", "Unknown Connector connection")
	}
	return parent, err
}

func (s *Store) ConfirmWorkbenchConnectorAccount(id string, generation int64) error {
	result, err := s.db.Exec(`UPDATE workbench_connector_connections SET auth_state='authorized',auth_generation=auth_generation+1 WHERE id=? AND auth_generation=?`, id, generation)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return common.Err("STALE_REVISION", "Connector account changed")
	}
	return err
}

const connectorScopeColumns = `c.id,c.connector_connection_id,c.scope_ref,c.access_state,c.config_revision,c.mapping_revision,c.snapshot_version,
	c.last_success,c.last_full_success,c.sync_checkpoint,c.config,p.connector_id,p.endpoint_ref,p.account_ref,p.auth_generation,p.auth_state`

func scanConnectorScope(row interface{ Scan(...any) error }) (common.WorkbenchConnectorStoredScope, error) {
	var scope common.WorkbenchConnectorStoredScope
	var last, full, checkpoint sql.NullString
	var raw string
	err := row.Scan(&scope.ScopeID, &scope.ConnectionID, &scope.ScopeRef, &scope.AccessState, &scope.ConfigRevision, &scope.MappingRevision, &scope.SnapshotVersion,
		&last, &full, &checkpoint, &raw, &scope.ConnectorID, &scope.Context.EndpointRef, &scope.Context.AccountRef, &scope.AuthGeneration, &scope.AuthState)
	if errors.Is(err, sql.ErrNoRows) {
		return scope, common.Err("NOT_FOUND", "Unknown Connector scope")
	}
	if err != nil {
		return scope, err
	}
	if last.Valid {
		at, err := time.Parse(time.RFC3339Nano, last.String)
		if err != nil {
			return scope, err
		}
		scope.LastSuccess = &at
	}
	if full.Valid {
		at, err := time.Parse(time.RFC3339Nano, full.String)
		if err != nil {
			return scope, err
		}
		scope.LastFullSuccess = &at
	}
	var config connectorScopeConfig
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return scope, err
	}
	scope.Checkpoint = checkpoint.String
	scope.Config = config.WorkbenchConnection
	scope.Mapping = config.Mapping
	scope.Context.ScopeRef = scope.ScopeRef
	scope.Context.EndpointConfig = config.EndpointConfig
	scope.Context.ScopeConfig = config.ScopeConfig
	scope.Context.EndpointSchemaVersion = config.EndpointSchemaVersion
	scope.Context.ScopeSchemaVersion = config.ScopeSchemaVersion
	return scope, nil
}

func (s *Store) WorkbenchConnectorScope(id string) (common.WorkbenchConnectorStoredScope, error) {
	return scanConnectorScope(s.db.QueryRow(`SELECT `+connectorScopeColumns+`
		FROM workbench_connections c JOIN workbench_connector_connections p ON p.id=c.connector_connection_id WHERE c.id=?`, id))
}

func (s *Store) WorkbenchConnectorOverviews(id string) ([]common.WorkbenchConnectorOverview, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := []common.WorkbenchConnectorOverview{}
	rows, err := tx.Query(`SELECT id,connector_id,endpoint_ref,auth_state FROM workbench_connector_connections WHERE (?='' OR id=?) ORDER BY id`, id, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		row := common.WorkbenchConnectorOverview{Scopes: []common.WorkbenchConnectorScopeOverview{}}
		p := &row.Connection
		if err := rows.Scan(&p.ID, &p.ConnectorID, &p.EndpointRef, &p.AuthState); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range result {
		rows, err := tx.Query(`SELECT `+connectorScopeColumns+`
			FROM workbench_connections c JOIN workbench_connector_connections p ON p.id=c.connector_connection_id
			WHERE p.id=? ORDER BY c.scope_ref`, result[i].Connection.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			scope, err := scanConnectorScope(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			result[i].Scopes = append(result[i].Scopes, common.WorkbenchConnectorScopeOverview{WorkbenchScopeState: scope.WorkbenchScopeState, Label: scope.Config.Name})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		for j := range result[i].Scopes {
			scope := &result[i].Scopes[j]
			run, err := readConnectorRun(tx, scope.ScopeID)
			if err != nil {
				return nil, err
			}
			scope.LastRun = run
		}
	}
	return result, nil
}

func (s *Store) DeleteWorkbenchConnectorConnection(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("DELETE FROM workbench_connections WHERE connector_connection_id=?", id); err != nil {
		return err
	}
	res, err := tx.Exec("DELETE FROM workbench_connector_connections WHERE id=?", id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return common.Err("NOT_FOUND", "Unknown Connector connection")
	}
	if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ConfigureWorkbenchConnectorScope(id, name string, version int64, write bool) error {
	result, err := s.db.Exec(`UPDATE workbench_connections SET config=json_set(config,'$.name',?,'$.write_enabled',json(?),'$.version',version+1),
		version=version+1,config_revision=config_revision+1
		WHERE id=? AND connector_connection_id IS NOT NULL AND version=?`, name, encode(write), id, version)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return common.Err("STALE_REVISION", "Scope configuration changed")
	}
	return err
}
