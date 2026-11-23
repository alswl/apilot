package dal

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

const connectorRunFrom = ` FROM workbench_sync_runs r
	JOIN workbench_connections c ON c.id=r.scope_id
	JOIN workbench_connector_connections p ON p.id=c.connector_connection_id`

const connectorRunSelect = `SELECT r.rowid,r.id,r.scope_id,p.id,p.connector_id,r.trigger,r.mode,r.status,r.started_at,r.finished_at,
	r.duration_ms,COALESCE(r.failed_stage,''),COALESCE(r.error_kind,''),r.retry_at,COALESCE(r.rate_limit_bucket,''),
	r.requests,r.pages,r.fetched,r.changed,r.rate_limits,r.usage_complete` + connectorRunFrom

const connectorRunFilter = ` WHERE (?='' OR p.connector_id=?) AND (?='' OR p.id=?) AND (?='' OR r.scope_id=?)
	AND (?='' OR r.mode=?) AND (?='' OR r.status=?)
	AND (?='' OR julianday(r.started_at)>=julianday(?)) AND (?='' OR julianday(r.started_at)<julianday(?))`

func connectorRunArgs(q common.WorkbenchSyncQuery) []any {
	return []any{q.ConnectorID, q.ConnectorID, q.ConnectionID, q.ConnectionID, q.ScopeID, q.ScopeID, q.Mode, q.Mode, q.Status, q.Status, q.From, q.From, q.To, q.To}
}

func scanConnectorRun(row interface{ Scan(...any) error }) (common.WorkbenchSyncRun, error) {
	var run common.WorkbenchSyncRun
	var start string
	var end, retry sql.NullString
	err := row.Scan(&run.LegacyID, &run.ID, &run.ScopeID, &run.ConnectionID, &run.ConnectorID, &run.Trigger, &run.Mode, &run.Status, &start, &end,
		&run.DurationMS, &run.FailedStage, &run.ErrorKind, &retry, &run.RateLimitBucket, &run.Requests, &run.Pages, &run.Fetched, &run.Changed, &run.RateLimits, &run.UsageComplete)
	if err != nil {
		return run, err
	}
	run.StartedAt, err = time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return run, err
	}
	for _, item := range []struct {
		raw  sql.NullString
		dest **time.Time
	}{{end, &run.FinishedAt}, {retry, &run.RetryAt}} {
		if item.raw.Valid {
			at, err := time.Parse(time.RFC3339Nano, item.raw.String)
			if err != nil {
				return run, err
			}
			*item.dest = &at
		}
	}
	return run, nil
}

func (s *Store) WorkbenchSyncRun(id string) (common.WorkbenchSyncRun, error) {
	run, err := scanConnectorRun(s.db.QueryRow(connectorRunSelect+" WHERE r.id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return run, common.Err("NOT_FOUND", "Unknown sync run")
	}
	return run, err
}

func (s *Store) WorkbenchSyncRuns(q common.WorkbenchSyncQuery) ([]common.WorkbenchSyncRun, error) {
	rows, err := s.db.Query(connectorRunSelect+connectorRunFilter+" ORDER BY r.rowid DESC LIMIT ? OFFSET ?", append(connectorRunArgs(q), q.Limit, q.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []common.WorkbenchSyncRun{}
	for rows.Next() {
		run, err := scanConnectorRun(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (s *Store) WorkbenchSyncUsage(q common.WorkbenchSyncQuery) ([]common.WorkbenchSyncUsage, error) {
	rows, err := s.db.Query(`SELECT p.connector_id,p.id,r.scope_id,r.mode,COUNT(*),SUM(r.status='success'),
		SUM(r.status NOT IN ('success','running')),SUM(r.status='running'),SUM(r.status='success' AND r.changed=0),
		SUM(r.requests),SUM(r.pages),SUM(r.fetched),SUM(r.changed),SUM(COALESCE(r.duration_ms,0)),SUM(r.rate_limits),
		SUM(r.usage_complete=0)`+connectorRunFrom+connectorRunFilter+`
		GROUP BY p.connector_id,p.id,r.scope_id,r.mode ORDER BY p.connector_id,p.id,r.scope_id,r.mode LIMIT ? OFFSET ?`,
		append(connectorRunArgs(q), q.Limit, q.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []common.WorkbenchSyncUsage{}
	for rows.Next() {
		var row common.WorkbenchSyncUsage
		if err := rows.Scan(&row.ConnectorID, &row.ConnectionID, &row.ScopeID, &row.Mode, &row.Runs, &row.Success, &row.Failed, &row.Running, &row.NoChanges,
			&row.Requests, &row.Pages, &row.Fetched, &row.Changed, &row.DurationMS, &row.RateLimits, &row.PartialRuns); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
