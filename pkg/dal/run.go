package dal

import (
	"database/sql"
	"encoding/json"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Store) SaveRun(r common.Run) error {
	var previous string
	err := s.db.QueryRow("SELECT status FROM runs WHERE id=?", r.ID).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && !common.CanTransition(previous, r.Status) {
		return common.Err("CONFLICT", "Illegal run status transition")
	}

	var task any = r.TaskID
	if r.TaskID == "" {
		task = nil
	}
	result, e := s.db.Exec("INSERT INTO runs(id,task_id,status,created_at,payload) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=excluded.status,payload=excluded.payload WHERE runs.status IN ('queued','running')", r.ID, task, r.Status, r.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"), encode(r))
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return common.Err("CONFLICT", "Run is already finalized")
	}
	return nil
}

func (s *Store) Runs() ([]common.Run, error) {
	rows, e := s.db.Query("SELECT payload FROM runs ORDER BY created_at DESC,id DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []common.Run{}
	for rows.Next() {
		var b string
		var r common.Run
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &r); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteRun(id string) error {
	_, e := s.db.Exec("DELETE FROM runs WHERE id=? AND status NOT IN ('queued','running')", id)
	return e
}
