package dal

import (
	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

func (s *Store) SaveWorkbenchConnectorMapping(scope common.WorkbenchConnectorStoredScope, rules map[string]protocol.MappingRule, tasks []common.WorkbenchTask) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE workbench_connections SET config=json_set(config,'$.mapping',json(?)),
		mapping_revision=mapping_revision+1,snapshot_version=snapshot_version+1,sync_checkpoint=NULL
		WHERE id=? AND mapping_revision=? AND snapshot_version=? AND config_revision=? AND access_state='available'
		AND EXISTS(SELECT 1 FROM workbench_connector_connections p WHERE p.id=connector_connection_id AND p.auth_state='authorized' AND p.auth_generation=?)`,
		encode(rules), scope.ScopeID, scope.MappingRevision, scope.SnapshotVersion, scope.ConfigRevision, scope.AuthGeneration)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return common.Err("STALE_REVISION", "Scope mapping or tasks changed")
	}
	for _, task := range tasks {
		if _, err = tx.Exec(`UPDATE workbench_objects SET title=?, state=?, payload=?
			WHERE id=? AND connection_id=? AND state<>'connector_hidden'`, task.Title, task.State, encode(task), task.ID, scope.ScopeID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
		return err
	}
	return tx.Commit()
}
