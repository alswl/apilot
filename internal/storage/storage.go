package storage

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/alswl/apilot/internal/model"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"
)

//go:embed migrations/001_core.sql
var schema string

//go:embed migrations/002_observers.sql
var observerSchema string

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: path}
	db, e := sql.Open("sqlite3", u.String()+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db}
	var version int
	if e = db.QueryRow("PRAGMA user_version").Scan(&version); e != nil {
		db.Close()
		return nil, e
	}
	if version > 2 {
		db.Close()
		return nil, fmt.Errorf("database schema %d is newer than supported 2", version)
	}
	if version == 0 {
		tx, e := db.Begin()
		if e != nil {
			db.Close()
			return nil, e
		}
		defer tx.Rollback()
		if _, e = tx.Exec(schema); e == nil {
			_, e = tx.Exec("PRAGMA user_version=1")
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			db.Close()
			return nil, e
		}
	}
	if version < 2 {
		tx, e := db.Begin()
		if e != nil {
			db.Close()
			return nil, e
		}
		defer tx.Rollback()
		if _, e = tx.Exec(observerSchema); e == nil {
			_, e = tx.Exec("PRAGMA user_version=2")
		}
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			db.Close()
			return nil, e
		}
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func encode(v any) string     { b, _ := json.Marshal(v); return string(b) }
func (s *Store) SaveTask(t model.Task) error {
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
func (s *Store) Tasks() ([]model.Task, error) {
	rows, e := s.db.Query("SELECT t.payload,t.env_json,COALESCE(s.payload,'null') FROM tasks t LEFT JOIN schedules s ON s.task_id=t.id ORDER BY t.name,t.id")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Task{}
	for rows.Next() {
		var payload, env, sch string
		if e = rows.Scan(&payload, &env, &sch); e != nil {
			return nil, e
		}
		var t model.Task
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
func (s *Store) SaveRun(r model.Run) error {
	var previous string
	err := s.db.QueryRow("SELECT status FROM runs WHERE id=?", r.ID).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && !model.CanTransition(previous, r.Status) {
		return model.Err("CONFLICT", "Illegal run status transition")
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
		return model.Err("CONFLICT", "Run is already finalized")
	}
	return nil
}
func (s *Store) Runs() ([]model.Run, error) {
	rows, e := s.db.Query("SELECT payload FROM runs ORDER BY created_at DESC,id DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Run{}
	for rows.Next() {
		var b string
		var r model.Run
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
func (s *Store) Settings() (model.Settings, error) {
	v := model.DefaultSettings()
	var b string
	e := s.db.QueryRow("SELECT payload FROM settings WHERE id=1").Scan(&b)
	if e == sql.ErrNoRows {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	e = json.Unmarshal([]byte(b), &v)
	return v, e
}
func (s *Store) SaveSettings(v model.Settings) error {
	_, e := s.db.Exec("INSERT INTO settings(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", encode(v))
	return e
}
func (s *Store) SaveEvent(v model.AttentionEvent) error {
	_, e := s.db.Exec("INSERT INTO attention_events(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", v.ID, encode(v))
	return e
}
func (s *Store) Events() ([]model.AttentionEvent, error) {
	rows, e := s.db.Query("SELECT payload FROM attention_events ORDER BY id DESC")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.AttentionEvent{}
	for rows.Next() {
		var b string
		var v model.AttentionEvent
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, rows.Err()
}

func (s *Store) Observers() ([]model.Observer, error) {
	rows, e := s.db.Query("SELECT payload,env_json FROM observers ORDER BY name")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []model.Observer{}
	for rows.Next() {
		var b, env string
		var v model.Observer
		if e = rows.Scan(&b, &env); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(b), &v); e != nil {
			return nil, e
		}
		if e = json.Unmarshal([]byte(env), &v.Env); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SaveObserver(v model.Observer) error {
	_, e := s.db.Exec("INSERT INTO observers(id,name,payload,env_json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,payload=excluded.payload,env_json=excluded.env_json", v.ID, v.Name, encode(v), encode(v.Env))
	return e
}
func (s *Store) DeleteObserver(id string) error {
	_, e := s.db.Exec("DELETE FROM observers WHERE id=?", id)
	return e
}
func (s *Store) AllowNotification(key string, now time.Time) (bool, error) {
	var b string
	e := s.db.QueryRow("SELECT last_sent_at FROM notification_limits WHERE source_key=?", key).Scan(&b)
	if e != nil && e != sql.ErrNoRows {
		return false, e
	}
	if e == nil {
		last, e := time.Parse(time.RFC3339Nano, b)
		if e == nil && now.Sub(last) < time.Minute {
			return false, nil
		}
	}
	_, e = s.db.Exec("INSERT INTO notification_limits(source_key,last_sent_at) VALUES(?,?) ON CONFLICT(source_key) DO UPDATE SET last_sent_at=excluded.last_sent_at", key, now.UTC().Format(time.RFC3339Nano))
	return e == nil, e
}
func (s *Store) DeleteRun(id string) error {
	_, e := s.db.Exec("DELETE FROM runs WHERE id=? AND status NOT IN ('queued','running')", id)
	return e
}
func (s *Store) DeleteEvent(id string) error {
	_, e := s.db.Exec("DELETE FROM attention_events WHERE id=?", id)
	return e
}
