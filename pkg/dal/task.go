package dal

import (
	"encoding/json"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Store) SaveTask(t common.Task) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	schedule := t.Schedule
	t.Schedule = nil
	t.LastRun = nil
	t.NextRunAt = nil
	_, e = tx.Exec("INSERT INTO tasks(id,name,payload,env_json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,payload=excluded.payload,env_json=excluded.env_json", t.ID, t.Name, encode(t), encode(t.Env))
	if e != nil {
		return e
	}
	if _, e = tx.Exec("DELETE FROM schedules WHERE task_id=?", t.ID); e != nil {
		return e
	}
	if schedule != nil {
		if _, e = tx.Exec("INSERT INTO schedules(id,task_id,payload) VALUES(?,?,?)", schedule.ID, t.ID, encode(schedule)); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s *Store) Tasks() ([]common.Task, error) {
	rows, e := s.db.Query("SELECT t.payload,t.env_json,COALESCE(s.payload,'null') FROM tasks t LEFT JOIN schedules s ON s.task_id=t.id ORDER BY t.name,t.id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []common.Task{}
	for rows.Next() {
		var payload, env, sch string
		if e = rows.Scan(&payload, &env, &sch); e != nil {
			return nil, e
		}
		var t common.Task
		if e = json.Unmarshal([]byte(payload), &t); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(env), &t.Env); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(sch), &t.Schedule); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTask(id string) error {
	_, e := s.db.Exec("DELETE FROM tasks WHERE id=?", id)
	return e
}
