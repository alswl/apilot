package dal

import (
	"database/sql"
	"encoding/json"

	"github.com/alswl/apilot/pkg/common"
)

func (s *Store) Settings() (common.Settings, error) {
	v := common.DefaultSettings()
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

func (s *Store) SaveSettings(v common.Settings) error {
	_, e := s.db.Exec("INSERT INTO settings(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", encode(v))
	return e
}
