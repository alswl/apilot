package dal

import (
	"database/sql"
	"errors"

	"github.com/alswl/apilot/core/pkg/common"
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

// RegisterWorkbenchPluginRelease records the discovered release and keeps the
// user's enablement preference. A missing release clears stale metadata.
func (s *Store) RegisterWorkbenchPluginRelease(id string, release *common.WorkbenchConnectorRelease) error {
	if release == nil {
		_, err := s.db.Exec(`UPDATE workbench_plugin_settings
			SET release_version=NULL, protocol_version=NULL, package_digest=NULL
			WHERE id=? AND release_version IS NOT NULL`, id)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO workbench_plugin_settings(id,enabled,release_version,protocol_version,package_digest)
		VALUES(?,1,?,?,?) ON CONFLICT(id) DO UPDATE SET release_version=excluded.release_version,
			protocol_version=excluded.protocol_version,package_digest=excluded.package_digest`,
		id, release.ReleaseVersion, release.ProtocolVersion, release.PackageDigest)
	return err
}

func (s *Store) WorkbenchPluginDigest(id string) (string, error) {
	var digest sql.NullString
	err := s.db.QueryRow("SELECT package_digest FROM workbench_plugin_settings WHERE id=?", id).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return digest.String, err
}
