package dal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func (s *Store) WorkbenchTaskReceipt(requestID string) (common.WorkbenchTaskReceipt, string, error) {
	var receipt common.WorkbenchTaskReceipt
	var fingerprint, raw string
	err := s.db.QueryRow("SELECT fingerprint, receipt FROM workbench_task_writes WHERE request_id=?", requestID).Scan(&fingerprint, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, "", nil
	}
	if err != nil {
		return receipt, "", err
	}
	return receipt, fingerprint, json.Unmarshal([]byte(raw), &receipt)
}

func (s *Store) CommitWorkbenchTaskWrite(connection common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, now time.Time, fingerprint string, receipt common.WorkbenchTaskReceipt) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recordWorkbench(tx, connection, attempts, before, after, &now); err != nil {
		return err
	}
	if _, err := tx.Exec("INSERT INTO workbench_task_writes(request_id,connection_id,fingerprint,receipt) VALUES(?,?,?,?) ON CONFLICT(request_id) DO UPDATE SET receipt=excluded.receipt",
		receipt.RequestID, receipt.ConnectionID, fingerprint, encode(receipt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) StageWorkbenchTaskWrite(fingerprint string, receipt common.WorkbenchTaskReceipt) error {
	_, err := s.db.Exec("INSERT INTO workbench_task_writes(request_id,connection_id,fingerprint,receipt) VALUES(?,?,?,?)",
		receipt.RequestID, receipt.ConnectionID, fingerprint, encode(receipt))
	return err
}

func (s *Store) SetWorkbenchTaskWriteState(receipt common.WorkbenchTaskReceipt, state string) error {
	receipt.SyncState = state
	_, err := s.db.Exec("UPDATE workbench_task_writes SET receipt=? WHERE request_id=? AND connection_id=?",
		encode(receipt), receipt.RequestID, receipt.ConnectionID)
	return err
}

func (s *Store) WorkbenchPendingWrites(connectionID string) ([]common.WorkbenchTaskReceipt, error) {
	rows, err := s.db.Query(`
		SELECT receipt FROM workbench_task_writes
		WHERE connection_id=? AND json_extract(receipt,'$.sync_state') IN ('needs_review','conflict')
		ORDER BY rowid DESC LIMIT 20`, connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pending []common.WorkbenchTaskReceipt
	for rows.Next() {
		var raw []byte
		var receipt common.WorkbenchTaskReceipt
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return nil, err
		}
		pending = append(pending, receipt)
	}
	return pending, rows.Err()
}
