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

//go:embed migrations/006_workbench_sync.sql
var workbenchSyncSchema string

//go:embed migrations/007_workbench_task_metadata.sql
var workbenchMetadataSchema string

//go:embed migrations/008_workbench_github.sql
var workbenchGitHubSchema string

//go:embed migrations/009_workbench_github_write_permission.sql
var workbenchGitHubWritePermissionSchema string

//go:embed migrations/010_workbench_github_poll.sql
var workbenchGitHubPollSchema string

//go:embed migrations/011_workbench_github_rate.sql
var workbenchGitHubRateSchema string

//go:embed migrations/012_workbench_connector_sync.sql
var workbenchConnectorSyncSchema string

//go:embed migrations/013_workbench_task_projection.sql
var workbenchTaskProjectionSchema string

//go:embed migrations/014_workbench_task_detail_cache.sql
var workbenchTaskDetailsSchema string

//go:embed migrations/015_workbench_sync_retry_bucket.sql
var workbenchSyncRetryBucketSchema string

//go:embed migrations/016_workbench_connector_endpoint_config.sql
var workbenchConnectorEndpointConfigSchema string

//go:embed migrations/017_workbench_connector_managed.sql
var workbenchConnectorManagedSchema string

//go:embed migrations/018_workbench_obsidian_task_fields.sql
var workbenchObsidianTaskFieldsSchema string

var migrations = []string{schema, observerSchema, workbenchSchema, workbenchLocalSchema, workbenchPluginSchema, workbenchSyncSchema, workbenchMetadataSchema, workbenchGitHubSchema, workbenchGitHubWritePermissionSchema, workbenchGitHubPollSchema, workbenchGitHubRateSchema, workbenchConnectorSyncSchema, workbenchTaskProjectionSchema, workbenchTaskDetailsSchema, workbenchSyncRetryBucketSchema, workbenchConnectorEndpointConfigSchema, workbenchConnectorManagedSchema, workbenchObsidianTaskFieldsSchema}

type Store struct{ db *sql.DB }

func sqliteURL(path, query string) string {
	u := url.URL{Scheme: "file", Path: path, RawQuery: query}
	return u.String()
}

func Open(path string) (*Store, error) {
	return openAtSchema(path, len(migrations))
}

func openAtSchema(path string, latest int) (*Store, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	f.Close()
	db, e := sql.Open("sqlite3", sqliteURL(path, "_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000"))
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
	if version > latest {
		db.Close()
		return nil, fmt.Errorf("database schema %d is newer than supported %d", version, latest)
	}
	for index, migration := range migrations[:latest] {
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
