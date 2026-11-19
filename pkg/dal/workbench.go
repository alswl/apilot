package dal

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Store) WorkbenchConnections() ([]common.WorkbenchSnapshot, error) {
	rows, err := s.db.Query("SELECT config,last_success,attempts FROM workbench_connections ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []common.WorkbenchSnapshot{}
	for rows.Next() {
		var config, attempts string
		var success sql.NullString
		var snapshot common.WorkbenchSnapshot
		if err := rows.Scan(&config, &success, &attempts); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(config), &snapshot.WorkbenchConnection); err != nil {
			return nil, err
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
		if _, err = tx.Exec("DELETE FROM workbench_objects WHERE connection_id=?", c.ID); err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RecordWorkbench(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success *time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("UPDATE workbench_connections SET attempts=?,last_success=COALESCE(?,last_success) WHERE id=? AND version=?", encode(attempts), formatWorkbenchTime(success), c.ID, c.Version)
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
	if success != nil {
		old := make(map[string]common.WorkbenchTask, len(before))
		next := make(map[string]bool, len(after))
		for _, task := range before {
			old[task.ID] = task
		}
		for _, task := range after {
			next[task.ID] = true
		}
		changed := false
		for _, task := range before {
			if next[task.ID] {
				continue
			}
			if _, err := tx.Exec("DELETE FROM workbench_objects WHERE id=?", task.ID); err != nil {
				return err
			}
			changed = true
		}
		for _, task := range after {
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
	return tx.Commit()
}

func formatWorkbenchTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}

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
	filter := " WHERE (?='' OR connection_id=?) AND (?='' OR state=?)"
	args := []any{q.Connection, q.Connection, q.State, q.State}
	if err := tx.QueryRow("SELECT COUNT(*) FROM workbench_objects"+filter, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := tx.Query("SELECT source_key,payload FROM workbench_objects"+filter+" ORDER BY title COLLATE NOCASE,id LIMIT ? OFFSET ?", append(args, q.Limit, q.Offset)...)
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
	return page, rows.Err()
}

func (s *Store) WorkbenchObject(id string) (common.WorkbenchTask, error) {
	task, err := scanWorkbench(s.db.QueryRow("SELECT source_key,payload FROM workbench_objects WHERE id=?", id))
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
