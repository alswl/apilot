package dal

import (
	"context"
	"database/sql"
)

// OpenChangeReader dedicates a separate SQLite connection to observing commits.
// PRAGMA data_version is comparable only on the same connection.
func OpenChangeReader(ctx context.Context, path string) (func(context.Context) (int64, error), func(), error) {
	db, err := sql.Open("sqlite3", sqliteURL(path, "mode=ro&_busy_timeout=250"))
	if err != nil {
		return nil, nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return func(ctx context.Context) (int64, error) {
			var version int64
			err := conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&version)
			return version, err
		}, func() {
			conn.Close()
			db.Close()
		}, nil
}
