package dal

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
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

func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
