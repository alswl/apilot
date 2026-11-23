package dal

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func (s *Store) WorkbenchConnections() ([]common.WorkbenchSnapshot, error) {
	// Read GitHub authorization with the same SQLite snapshot as its configuration and attempts.
	rows, err := s.db.Query(`SELECT c.config,c.last_success,c.attempts,c.snapshot_version,
		g.account_id,g.account_login,g.access_state,g.write_denied,c.connector_connection_id,c.scope_ref,c.access_state
		FROM workbench_connections c LEFT JOIN workbench_github_connections g ON g.connection_id=c.id
		ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []common.WorkbenchSnapshot{}
	for rows.Next() {
		var config, attempts string
		var success, accountLogin, accessState, parentID, scopeRef sql.NullString
		var scopeAccess string
		var accountID sql.NullInt64
		var writeDenied sql.NullBool
		var snapshot common.WorkbenchSnapshot
		if err := rows.Scan(&config, &success, &attempts, &snapshot.SnapshotVersion, &accountID, &accountLogin, &accessState, &writeDenied, &parentID, &scopeRef, &scopeAccess); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(config), &snapshot.WorkbenchConnection); err != nil {
			return nil, err
		}
		if parentID.Valid {
			snapshot.ParentConnectionID, snapshot.ScopeRef, snapshot.AccessState = parentID.String, scopeRef.String, scopeAccess
		}
		if snapshot.Kind == common.WorkbenchGitHubRepository {
			if !accountID.Valid {
				return nil, common.Err("INTERNAL", "GitHub connection storage unavailable")
			}
			snapshot.GitHubAccountID = accountID.Int64
			snapshot.AccountLogin = accountLogin.String
			snapshot.AccessState = accessState.String
			snapshot.WriteDenied = writeDenied.Bool
		}
		if err := json.Unmarshal([]byte(attempts), &snapshot.Attempts); err != nil {
			return nil, err
		}
		if success.Valid {
			at, err := time.Parse(time.RFC3339Nano, success.String)
			if err != nil {
				return nil, err
			}
			snapshot.LastSuccess = &at
		}
		out = append(out, snapshot)
	}
	return out, rows.Err()
}

func (s *Store) SaveWorkbench(c common.WorkbenchConnection, reset bool) error {
	return s.SaveWorkbenchBridge(c, reset, nil)
}

func (s *Store) SaveWorkbenchBridge(c common.WorkbenchConnection, reset bool, bridge *common.WorkbenchBridgeInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO workbench_connections(id,config,version) VALUES(?,?,?)
 ON CONFLICT(id) DO UPDATE SET config=excluded.config,version=excluded.version,
 last_success=CASE WHEN ? THEN NULL ELSE last_success END`, c.ID, encode(c), c.Version, reset)
	if err != nil {
		return err
	}
	if reset {
		// Historic locations may be outside the newly authorized scope.
		var rawAttempts string
		if err := tx.QueryRow("SELECT attempts FROM workbench_connections WHERE id=?", c.ID).Scan(&rawAttempts); err != nil {
			return err
		}
		var attempts []common.WorkbenchAttempt
		if err := json.Unmarshal([]byte(rawAttempts), &attempts); err != nil {
			return err
		}
		for i := range attempts {
			attempts[i].Paths = nil
		}
		if _, err := tx.Exec("UPDATE workbench_connections SET attempts=? WHERE id=?", encode(attempts), c.ID); err != nil {
			return err
		}
		if _, err = tx.Exec("DELETE FROM workbench_objects WHERE connection_id=?", c.ID); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	if bridge != nil {
		if _, err := tx.Exec(`INSERT INTO workbench_obsidian_bridges(connection_id,endpoint,token,certificate)
			VALUES(?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET endpoint=excluded.endpoint,
			token=excluded.token, certificate=excluded.certificate`, c.ID, bridge.Endpoint, bridge.Token, bridge.Certificate); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) RecordWorkbench(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success *time.Time) error {
	return s.recordWorkbenchVersion(c, attempts, before, after, success, nil)
}

// RecordWorkbenchIfVersion rejects observations based on an older snapshot.
func (s *Store) RecordWorkbenchIfVersion(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success *time.Time, version int64) error {
	return s.recordWorkbenchVersion(c, attempts, before, after, success, &version)
}

// RecordWorkbenchPartialSnapshot publishes the verified portion of a full
// observation without treating unresolved identities as a full confirmation.
func (s *Store) RecordWorkbenchPartialSnapshot(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, observed time.Time, version int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	if err := tx.QueryRow("SELECT snapshot_version FROM workbench_connections WHERE id=?", c.ID).Scan(&current); err != nil {
		return err
	}
	if current != version {
		return common.Err("STALE_REVISION", "Source snapshot changed")
	}
	if err := recordWorkbenchSnapshot(tx, c, attempts, before, after, nil, &observed); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) recordWorkbenchVersion(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success *time.Time, version *int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version != nil {
		var current int64
		if err := tx.QueryRow("SELECT snapshot_version FROM workbench_connections WHERE id=?", c.ID).Scan(&current); err != nil {
			return err
		}
		if current != *version {
			return common.Err("STALE_REVISION", "Source snapshot changed")
		}
	}
	if err := recordWorkbench(tx, c, attempts, before, after, success); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CommitWorkbenchImport(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success *time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("INSERT INTO workbench_connections(id,config,version) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING", c.ID, encode(c), c.Version); err != nil {
		return err
	}
	if err := recordWorkbench(tx, c, attempts, before, after, success); err != nil {
		return err
	}
	return tx.Commit()
}

func recordWorkbench(tx *sql.Tx, c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success *time.Time) error {
	return recordWorkbenchSnapshot(tx, c, attempts, before, after, success, success)
}

func recordWorkbenchSnapshot(tx *sql.Tx, c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success, observed *time.Time) error {
	result, err := tx.Exec("UPDATE workbench_connections SET attempts=?,last_success=COALESCE(?,last_success),snapshot_version=snapshot_version+? WHERE id=? AND version=?", encode(attempts), formatWorkbenchTime(success), observed != nil, c.ID, c.Version)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return common.Err("STALE_REVISION", "Connection configuration changed")
	}
	if observed != nil {
		old := make(map[string]common.WorkbenchTask, len(before))
		next := make(map[string]string, len(after))
		for _, task := range before {
			old[task.ID] = task
		}
		for _, task := range after {
			next[task.ID] = task.SourceKey
		}
		changed := false
		// Release old keys before writing rows that may now own those keys.
		for _, task := range before {
			if key, exists := next[task.ID]; exists && key == task.SourceKey {
				continue
			}
			if _, err := tx.Exec("DELETE FROM workbench_objects WHERE id=?", task.ID); err != nil {
				return err
			}
			changed = true
		}
		for _, task := range after {
			if _, err := tx.Exec("INSERT INTO workbench_tasks(id,created_at) VALUES(?,?) ON CONFLICT(id) DO NOTHING", task.ID, formatWorkbenchTime(observed)); err != nil {
				return err
			}
			payload := encode(task)
			if previous, ok := old[task.ID]; ok && previous.SourceKey == task.SourceKey && encode(previous) == payload {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO workbench_objects(id,connection_id,source_key,title,state,payload)
     VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET source_key=excluded.source_key,title=excluded.title,
     state=excluded.state,payload=excluded.payload`, task.ID, c.ID, task.SourceKey, task.Title, task.State, payload); err != nil {
				return err
			}
			changed = true
		}
		if changed {
			if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatWorkbenchTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}

const workbenchObjectVisible = `NOT EXISTS (SELECT 1 FROM workbench_github_connections g WHERE g.connection_id=workbench_objects.connection_id AND g.access_state<>'available')
 AND state<>'connector_hidden'
 AND NOT EXISTS (SELECT 1 FROM workbench_connections c
 JOIN workbench_connector_connections p ON p.id=c.connector_connection_id
 WHERE c.id=workbench_objects.connection_id AND (c.access_state<>'available' OR p.auth_state<>'authorized'))`

func (s *Store) WorkbenchPage(q common.WorkbenchQuery) (common.WorkbenchPage, error) {
	tx, err := s.db.Begin()
	page := common.WorkbenchPage{Items: []common.WorkbenchTask{}}
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err := tx.QueryRow("SELECT revision FROM workbench_revision").Scan(&page.Revision); err != nil {
		return page, err
	}
	if q.Revision != 0 && page.Revision != q.Revision {
		return page, common.Err("STALE_REVISION", "Task list changed")
	}
	scope := q.Connection
	if q.ScopeID != "" {
		scope = q.ScopeID
	}
	filter := ` WHERE ` + workbenchObjectVisible + ` AND (?='' OR connection_id=?) AND (?='' OR state=?)
		AND (?='' OR source_kind=?) AND (? IS NULL OR priority=?)
		AND (?='' OR due_date>=?) AND (?='' OR due_date<=?)
		AND (?='' OR EXISTS(SELECT 1 FROM json_each(payload,'$.assignees') a WHERE a.value=? COLLATE NOCASE))
		AND (?='' OR EXISTS (SELECT 1 FROM workbench_task_labels WHERE task_id=workbench_objects.id AND label_id=?))`
	args := []any{scope, scope, q.State, q.State, q.Source, q.Source, q.Priority, q.Priority,
		q.DueAfter, q.DueAfter, q.DueBefore, q.DueBefore, q.Assignee, q.Assignee, q.LabelID, q.LabelID}
	if err := tx.QueryRow("SELECT COUNT(*) FROM workbench_objects"+filter, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	column := "title COLLATE NOCASE"
	switch q.Sort {
	case "priority":
		column = "priority"
	case "due_date":
		column = "due_date"
	case "state":
		column = "state"
	case "assignee":
		column = "json_extract(payload,'$.assignees[0]') COLLATE NOCASE"
	}
	direction := " ASC"
	if q.Descending {
		direction = " DESC"
	}
	rows, err := tx.Query("SELECT source_key,payload FROM workbench_objects"+filter+" ORDER BY ("+column+") IS NULL,"+column+direction+",id LIMIT ? OFFSET ?", append(args, q.Limit, q.Offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		task, err := scanWorkbench(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, task)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if err := rows.Close(); err != nil {
		return page, err
	}
	ids := make([]string, len(page.Items))
	for i := range page.Items {
		ids[i] = page.Items[i].ID
	}
	labels, err := workbenchTaskLabels(tx, ids)
	if err != nil {
		return page, err
	}
	for i := range page.Items {
		page.Items[i].WorkbenchLabels = labels[page.Items[i].ID]
	}
	return page, nil
}

func (s *Store) WorkbenchObject(id string) (common.WorkbenchTask, error) {
	task, err := scanWorkbench(s.db.QueryRow("SELECT source_key,payload FROM workbench_objects WHERE id=? AND "+workbenchObjectVisible, id))
	if err == sql.ErrNoRows {
		return task, common.Err("NOT_FOUND", "Unknown workbench task")
	}
	return task, err
}

func scanWorkbench(row interface{ Scan(...any) error }) (common.WorkbenchTask, error) {
	var task common.WorkbenchTask
	var key, payload string
	if err := row.Scan(&key, &payload); err != nil {
		return task, err
	}
	err := json.Unmarshal([]byte(payload), &task)
	task.SourceKey = key
	return task, err
}

func (s *Store) WorkbenchObjects(connection string) ([]common.WorkbenchTask, error) {
	rows, err := s.db.Query("SELECT source_key,payload FROM workbench_objects WHERE connection_id=?", connection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []common.WorkbenchTask
	for rows.Next() {
		task, err := scanWorkbench(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) DeleteWorkbench(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("DELETE FROM workbench_connections WHERE id=?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return common.Err("NOT_FOUND", "Unknown workbench connection")
	}
	if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitWorkbenchObservedTasks updates only previously mapped source objects.
// It never advances the last successful full-scope verification time.
func (s *Store) CommitWorkbenchObservedTasks(connection common.WorkbenchConnection, version int64, tasks []common.WorkbenchTask) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := commitWorkbenchObservedTasks(tx, connection, version, tasks, false); err != nil {
		return err
	}
	return tx.Commit()
}

func commitWorkbenchObservedTasks(tx *sql.Tx, connection common.WorkbenchConnection, version int64, tasks []common.WorkbenchTask, allowPaused bool) error {
	result, err := tx.Exec(`UPDATE workbench_connections SET snapshot_version=snapshot_version+1
WHERE id=? AND version=? AND snapshot_version=? AND (? OR COALESCE(json_extract(config,'$.paused'),0)=0)
AND COALESCE((SELECT enabled FROM workbench_plugin_settings WHERE id=json_extract(config,'$.kind')),1)=1`, connection.ID, connection.Version, version, allowPaused)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return common.Err("STALE_REVISION", "Connection or source snapshot changed")
	}
	changed := false
	for _, task := range tasks {
		if task.ConnectionID != connection.ID {
			return common.Err("INVALID_ARGUMENT", "Task belongs to another connection")
		}
		result, err := tx.Exec(`UPDATE workbench_objects SET title=?,state=?,payload=?
WHERE id=? AND connection_id=? AND source_key=? AND (title<>? OR state<>? OR payload<>?)`, task.Title, task.State, encode(task), task.ID, connection.ID, task.SourceKey, task.Title, task.State, encode(task))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var exists int
			if err := tx.QueryRow("SELECT 1 FROM workbench_objects WHERE id=? AND connection_id=? AND source_key=?", task.ID, connection.ID, task.SourceKey).Scan(&exists); err != nil {
				if err == sql.ErrNoRows {
					return common.Err("STALE_REVISION", "Task mapping changed")
				}
				return err
			}
		} else {
			changed = true
		}
	}
	if changed {
		if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	return nil
}

// FinishWorkbenchAttempt updates an in-flight attempt without replacing newer history.
func (s *Store) FinishWorkbenchAttempt(connection common.WorkbenchConnection, attempt common.WorkbenchAttempt) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRow("SELECT attempts FROM workbench_connections WHERE id=? AND version=?", connection.ID, connection.Version).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return common.Err("STALE_REVISION", "Connection configuration changed")
		}
		return err
	}
	var attempts []common.WorkbenchAttempt
	if err := json.Unmarshal([]byte(raw), &attempts); err != nil {
		return err
	}
	found := false
	for i := range attempts {
		if attempts[i].ID == attempt.ID && attempts[i].State == "running" {
			attempts[i] = attempt
			found = true
			break
		}
	}
	if !found {
		return common.Err("STALE_REVISION", "Sync attempt changed")
	}
	if _, err := tx.Exec("UPDATE workbench_connections SET attempts=? WHERE id=? AND version=?", encode(attempts), connection.ID, connection.Version); err != nil {
		return err
	}
	return tx.Commit()
}
