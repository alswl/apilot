package dal

import (
	"database/sql"
	"time"
)

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
