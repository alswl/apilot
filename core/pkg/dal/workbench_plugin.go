package dal

import (
	"database/sql"
	"errors"
)

func (s *Store) WorkbenchPluginEnabled(id string) (bool, error) {
	var enabled bool
	err := s.db.QueryRow("SELECT enabled FROM workbench_plugin_settings WHERE id = ?", id).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	return enabled, err
}

func (s *Store) SetWorkbenchPluginEnabled(id string, enabled bool) error {
	_, err := s.db.Exec(`INSERT INTO workbench_plugin_settings(id,enabled) VALUES(?,?)
		ON CONFLICT(id) DO UPDATE SET enabled = excluded.enabled`, id, enabled)
	return err
}
