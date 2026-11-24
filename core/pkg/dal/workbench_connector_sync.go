package dal

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

const connectorHidden = "connector_hidden"

func (s *Store) RecoverWorkbenchConnectorRuns() error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`UPDATE workbench_sync_runs SET status='interrupted',finished_at=?,duration_ms=MAX(0,CAST((julianday(?) - julianday(started_at))*86400000 AS INTEGER)),error_kind='process_failure',failed_stage='interrupted'
		WHERE status='running'`, formatWorkbenchTime(&now), formatWorkbenchTime(&now))
	return err
}

func (s *Store) StartWorkbenchConnectorRun(run common.WorkbenchSyncRun) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO workbench_sync_runs(id,scope_id,trigger,mode,status,started_at)
		VALUES(?,?,?,?,?,?)`, run.ID, run.ScopeID, run.Trigger, run.Mode, run.Status, formatWorkbenchTime(&run.StartedAt))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func readConnectorRun(tx *sql.Tx, id string) (*common.WorkbenchSyncRun, error) {
	run, err := scanConnectorRun(tx.QueryRow(connectorRunSelect+" WHERE r.scope_id=? ORDER BY r.rowid DESC LIMIT 1", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

func finishConnectorRun(tx *sql.Tx, run common.WorkbenchSyncRun) error {
	res, err := tx.Exec(`UPDATE workbench_sync_runs SET status=?,finished_at=?,duration_ms=?,failed_stage=?,error_kind=?,
		retry_at=?,rate_limit_bucket=?,requests=?,pages=?,fetched=?,changed=?,rate_limits=?,usage_complete=? WHERE id=? AND status='running'`,
		run.Status, formatWorkbenchTime(run.FinishedAt), run.DurationMS, run.FailedStage, run.ErrorKind,
		formatWorkbenchTime(run.RetryAt), run.RateLimitBucket, run.Requests, run.Pages, run.Fetched, run.Changed, run.RateLimits, run.UsageComplete, run.ID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return common.Err("STALE_REVISION", "Sync run already finished")
	}
	return err
}

func connectorSyncCurrent(tx *sql.Tx, c common.WorkbenchConnectorSyncCommit) (bool, error) {
	var valid bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM workbench_connections s
		JOIN workbench_connector_connections p ON p.id=s.connector_connection_id
		JOIN workbench_plugin_settings v ON v.id=?
		WHERE s.id=? AND s.config_revision=? AND s.mapping_revision=? AND s.snapshot_version=?
		AND p.auth_generation=? AND v.enabled=1 AND v.release_version=? AND v.protocol_version=? AND v.package_digest=?
		AND COALESCE(json_extract(s.config,'$.paused'),0)=0)`,
		c.PluginID, c.Scope.ScopeID, c.Scope.ConfigRevision, c.Scope.MappingRevision, c.Scope.SnapshotVersion, c.Scope.AuthGeneration,
		c.Release.ReleaseVersion, c.Release.ProtocolVersion, c.Release.PackageDigest).Scan(&valid)
	return valid, err
}

func (s *Store) FinishWorkbenchConnectorFailure(c common.WorkbenchConnectorSyncCommit) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := connectorSyncCurrent(tx, c)
	if err != nil {
		return err
	}
	changed := false
	if current && c.Run.ErrorKind == "auth_required" {
		_, err = tx.Exec(`UPDATE workbench_connector_connections SET auth_state='auth_required',auth_generation=auth_generation+1 WHERE id=?`, c.Scope.ConnectionID)
		if err == nil {
			_, err = tx.Exec("UPDATE workbench_connections SET access_state='auth_required' WHERE connector_connection_id=?", c.Scope.ConnectionID)
		}
		changed = true
	} else if current && c.Run.ErrorKind == "access_denied" {
		_, err = tx.Exec(`UPDATE workbench_connections SET access_state='access_denied',snapshot_version=snapshot_version+1 WHERE id=?`, c.Scope.ScopeID)
		changed = true
	}
	if err != nil {
		return err
	}

	if changed {
		if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	if err := finishConnectorRun(tx, c.Run); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CommitWorkbenchConnectorSync(c common.WorkbenchConnectorSyncCommit) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := connectorSyncCurrent(tx, c)
	if err != nil {
		return err
	}
	if !current {
		return common.Err("STALE_REVISION", "Connector scope changed during sync")
	}
	for key, task := range c.Records {
		var existingID, payload, state string
		err := tx.QueryRow("SELECT id,payload,state FROM workbench_objects WHERE connection_id=? AND source_key=?", c.Scope.ScopeID, key).Scan(&existingID, &payload, &state)
		switch {
		case err == nil:
			task.ID = existingID
			if state == task.State && payload == encode(task) {
				continue
			}
		case errors.Is(err, sql.ErrNoRows):
			task.ID = common.ID()
			if _, err := tx.Exec("INSERT INTO workbench_tasks(id,created_at) VALUES(?,?)", task.ID, formatWorkbenchTime(c.Run.FinishedAt)); err != nil {
				return err
			}
		default:
			return err
		}
		if _, err := tx.Exec(`INSERT INTO workbench_objects(id,connection_id,source_key,title,state,payload)
			VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,state=excluded.state,payload=excluded.payload`,
			task.ID, c.Scope.ScopeID, key, task.Title, task.State, encode(task)); err != nil {
			return err
		}
		c.Run.Changed++
	}
	for key := range c.Removed {
		res, err := tx.Exec("UPDATE workbench_objects SET state=? WHERE connection_id=? AND source_key=? AND state<>?", connectorHidden, c.Scope.ScopeID, key, connectorHidden)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		c.Run.Changed += int(n)
	}
	var full any
	if c.Run.Mode == "full" {
		full = formatWorkbenchTime(c.Run.FinishedAt)
	}
	if _, err := tx.Exec(`UPDATE workbench_connections SET sync_checkpoint=?,last_success=?,last_full_success=COALESCE(?,last_full_success),
		access_state='available',snapshot_version=snapshot_version+1 WHERE id=?`, c.Checkpoint, formatWorkbenchTime(c.Run.FinishedAt), full, c.Scope.ScopeID); err != nil {
		return err
	}
	if c.Run.Changed > 0 || c.Scope.AccessState != "available" {
		if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	if err := finishConnectorRun(tx, c.Run); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) WorkbenchConnectorMembers(id string) ([]common.WorkbenchTask, error) {
	rows, err := s.db.Query("SELECT source_key,payload FROM workbench_objects WHERE connection_id=? AND state<>?", id, connectorHidden)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []common.WorkbenchTask
	for rows.Next() {
		task, err := scanWorkbench(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, task)
	}
	return members, rows.Err()
}

func (s *Store) WorkbenchConnectorLastRun(id string) (*common.WorkbenchSyncRun, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return readConnectorRun(tx, id)
}

func (s *Store) WorkbenchConnectorBackoff(parentID, scopeID string, buckets []string) (*time.Time, error) {
	var raw sql.NullString
	err := s.db.QueryRow(`SELECT r.retry_at FROM workbench_sync_runs r JOIN workbench_connections c
		ON r.scope_id=c.id WHERE c.connector_connection_id=? AND r.error_kind='rate_limited' AND r.retry_at IS NOT NULL
		AND (COALESCE(r.rate_limit_bucket,'') NOT GLOB 'scope:*' OR r.scope_id=?)
		AND (? OR COALESCE(r.rate_limit_bucket,'') IN ('','scope:')
			OR r.rate_limit_bucket IN (SELECT value FROM json_each(?))
			OR r.rate_limit_bucket IN (SELECT 'scope:' || value FROM json_each(?)))
		ORDER BY julianday(r.retry_at) DESC LIMIT 1`, parentID, scopeID, buckets == nil, encode(buckets), encode(buckets)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || !raw.Valid {
		return nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, raw.String)
	return &at, err
}

func (s *Store) PauseWorkbenchConnectorScope(id string, version int64, paused bool) error {
	result, err := s.db.Exec(`UPDATE workbench_connections SET config=json_set(config,'$.paused',json(?),'$.version',version+1),
		version=version+1,config_revision=config_revision+1 WHERE id=? AND connector_connection_id IS NOT NULL AND version=?`, encode(paused), id, version)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return common.Err("STALE_REVISION", "Connector scope changed")
	}
	return err
}
