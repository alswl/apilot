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

//go:embed migrations/003_workbench.sql
var workbenchSchema string

//go:embed migrations/004_workbench_local.sql
var workbenchLocalSchema string

//go:embed migrations/005_workbench_plugins.sql
var workbenchPluginSchema string

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
	if version > 5 {
		db.Close()
		return nil, fmt.Errorf("database schema %d is newer than supported 5", version)
	}
	for index, migration := range []string{schema, observerSchema, workbenchSchema, workbenchLocalSchema, workbenchPluginSchema} {
		next := index + 1
		if next <= version {
			continue
		}
		if err := applyMigration(db, next, migration); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate database to schema %d: %w", next, err)
		}
	}
	return s, nil
}

func applyMigration(db *sql.DB, version int, schema string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", version)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

func encode(v any) string { b, _ := json.Marshal(v); return string(b) }
