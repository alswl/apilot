package dal

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/alswl/apilot/core/pkg/common"
)

const detailsCurrent = `SELECT EXISTS(SELECT 1 FROM workbench_objects o
	JOIN workbench_connections c ON c.id=o.connection_id
	JOIN workbench_connector_connections p ON p.id=c.connector_connection_id
	WHERE o.id=? AND o.state<>'connector_hidden' AND c.access_state='available'
	AND p.auth_state='authorized' AND p.auth_generation=? AND c.config_revision=? AND c.mapping_revision=?
	AND json_extract(o.payload,'$.native_fields.source_version')=?)`

func (s *Store) ReadWorkbenchTaskDetail(scope common.WorkbenchConnectorStoredScope, taskID, version, digest, segment, page string) (common.WorkbenchTaskDetail, error) {
	var result common.WorkbenchTaskDetail
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM workbench_task_detail_cache
		WHERE task_id=? AND source_version=? AND auth_generation=? AND config_revision=? AND package_digest=?
		AND segment=? AND page_key=? AND (`+detailsCurrent+`)`,
		taskID, version, scope.AuthGeneration, scope.ConfigRevision, digest, segment, page, taskID, scope.AuthGeneration, scope.ConfigRevision, scope.MappingRevision, version).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return result, common.Err("NOT_FOUND", "Cached detail unavailable")
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(payload), &result)
	result.Cached = true
	return result, err
}

func (s *Store) SaveWorkbenchTaskDetail(scope common.WorkbenchConnectorStoredScope, version, digest, page string, result common.WorkbenchTaskDetail) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current bool
	if err := tx.QueryRow(detailsCurrent, result.TaskID, scope.AuthGeneration, scope.ConfigRevision, scope.MappingRevision, version).Scan(&current); err != nil {
		return err
	}
	if !current {
		return common.Err("STALE_REVISION", "Task or scope changed during detail read")
	}
	if version != result.Version {
		return tx.Commit()
	}
	_, err = tx.Exec(`INSERT INTO workbench_task_detail_cache(task_id,segment,page_key,source_version,auth_generation,config_revision,package_digest,captured_at,payload)
		VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(task_id,segment,page_key) DO UPDATE SET source_version=excluded.source_version,
		auth_generation=excluded.auth_generation,config_revision=excluded.config_revision,package_digest=excluded.package_digest,
		captured_at=excluded.captured_at,payload=excluded.payload`,
		result.TaskID, result.Segment, page, version, scope.AuthGeneration, scope.ConfigRevision, digest, formatWorkbenchTime(&result.CapturedAt), encode(result))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RevokeWorkbenchTaskDetail(scope common.WorkbenchConnectorStoredScope, taskID, version, level string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current bool
	if err := tx.QueryRow(detailsCurrent, taskID, scope.AuthGeneration, scope.ConfigRevision, scope.MappingRevision, version).Scan(&current); err != nil {
		return err
	}
	if !current {
		return nil
	}
	switch level {
	case "account", "endpoint":
		_, err = tx.Exec("UPDATE workbench_connector_connections SET auth_state='auth_required',auth_generation=auth_generation+1 WHERE id=?", scope.ConnectionID)
		if err == nil {
			_, err = tx.Exec("UPDATE workbench_connections SET access_state='auth_required' WHERE connector_connection_id=?", scope.ConnectionID)
		}
	case "scope":
		_, err = tx.Exec("UPDATE workbench_connections SET access_state='access_denied',snapshot_version=snapshot_version+1 WHERE id=?", scope.ScopeID)
	case "object":
		_, err = tx.Exec("UPDATE workbench_objects SET state='connector_hidden' WHERE id=?", taskID)
		if err == nil {
			_, err = tx.Exec("UPDATE workbench_connections SET snapshot_version=snapshot_version+1,sync_checkpoint=NULL WHERE id=?", scope.ScopeID)
		}
	default:
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM workbench_task_detail_cache WHERE task_id=?", taskID); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
		return err
	}
	return tx.Commit()
}
