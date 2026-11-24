package dal

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

func (s *Store) RecordWorkbenchConnectorEvidence(id string, evidence protocol.MutationEvidence) error {
	_, err := s.db.Exec(`UPDATE workbench_task_writes SET receipt=json_set(receipt,'$.evidence',json(?))
		WHERE request_id=? AND phase='dispatching' AND json_extract(input,'$.binding.connector') IS NOT NULL`, encode(evidence), id)
	return err
}

func (s *Store) CommitWorkbenchConnectorOperation(c common.WorkbenchConnectorSyncCommit, from string, task common.WorkbenchTask, receipt common.WorkbenchTaskReceipt) (common.WorkbenchTaskReceipt, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	current, err := connectorSyncCurrent(tx, c)
	if err != nil {
		return receipt, err
	}
	if !current {
		return receipt, common.Err("STALE_REVISION", "Scope changed before operation confirmation")
	}
	op, err := lookupWorkbenchTaskOperation(tx, receipt.RequestID)
	if err != nil {
		return receipt, err
	}
	if op.Binding == nil || op.Binding.Connector == nil || op.Receipt.ConnectionID != c.Scope.ScopeID || op.Phase != from {
		return receipt, common.Err("STALE_REVISION", "Operation changed during confirmation")
	}
	var existing string
	err = tx.QueryRow(`SELECT id FROM workbench_objects WHERE connection_id=? AND source_key=? AND state<>'connector_hidden'`,
		task.ConnectionID, task.SourceKey).Scan(&existing)
	switch {
	case err == nil:
		if op.Input.SourceObjectID != "" && op.Input.SourceObjectID != existing {
			return receipt, common.Err("CONFLICT", "Task binding changed")
		}
		task.ID = existing
	case errors.Is(err, sql.ErrNoRows):
		if op.Input.SourceObjectID != "" {
			return receipt, common.Err("CONFLICT", "Task is no longer visible")
		}
		task.ID = common.ID()
		now := time.Now().UTC()
		if _, err := tx.Exec("INSERT INTO workbench_tasks(id,created_at) VALUES(?,?)", task.ID, formatWorkbenchTime(&now)); err != nil {
			return receipt, err
		}
	default:
		return receipt, err
	}
	_, err = tx.Exec(`INSERT INTO workbench_objects(id,connection_id,source_key,title,state,payload)
		VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,state=excluded.state,payload=excluded.payload`,
		task.ID, task.ConnectionID, task.SourceKey, task.Title, task.State, encode(task))
	if err != nil {
		return receipt, err
	}
	receipt.SourceObjectID, receipt.SyncState = task.ID, common.WorkbenchOperationConfirmed
	if _, err := tx.Exec("UPDATE workbench_task_writes SET phase='confirmed',receipt=? WHERE request_id=?", encode(receipt), receipt.RequestID); err != nil {
		return receipt, err
	}
	if _, err := tx.Exec("UPDATE workbench_connections SET snapshot_version=snapshot_version+1 WHERE id=?", task.ConnectionID); err != nil {
		return receipt, err
	}
	if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
		return receipt, err
	}
	return receipt, tx.Commit()
}
