package dal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchStoredOperationInput struct {
	Request common.WorkbenchTaskWrite         `json:"request"`
	Binding *common.WorkbenchOperationBinding `json:"binding,omitempty"`
}

func decodeWorkbenchOperationInput(raw string, op *common.WorkbenchTaskOperation) error {
	var envelope struct {
		Request json.RawMessage                   `json:"request"`
		Binding *common.WorkbenchOperationBinding `json:"binding"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return err
	}
	if len(envelope.Request) == 0 {
		return json.Unmarshal([]byte(raw), &op.Input)
	}
	if err := json.Unmarshal(envelope.Request, &op.Input); err != nil {
		return err
	}
	op.Binding = envelope.Binding
	return nil
}

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
		WHERE connection_id=? AND NOT EXISTS (SELECT 1 FROM workbench_github_connections g WHERE g.connection_id=workbench_task_writes.connection_id AND (g.access_state<>'available' OR COALESCE(json_extract(workbench_task_writes.input,'$.binding.account_id'),0)<>g.account_id OR COALESCE(json_extract(workbench_task_writes.input,'$.binding.repository_id'),0)<>g.repository_id)) AND (
			json_extract(receipt,'$.sync_state') IN ('accepted','dispatching','needs_review')
			OR request_id IN (
				SELECT request_id FROM workbench_task_writes
				WHERE connection_id=? AND json_extract(receipt,'$.sync_state') IN ('conflict','not_executed')
				ORDER BY rowid DESC LIMIT 20
			)
		)
		ORDER BY rowid DESC`, connectionID, connectionID)
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

// AdmitWorkbenchTaskOperation persists the complete request before a worker can dispatch it.
// Reusing a request ID is idempotent only when its original fingerprint matches.
func (s *Store) AdmitWorkbenchTaskOperation(input common.WorkbenchTaskWrite, connectionID, fingerprint string) (common.WorkbenchTaskOperation, bool, error) {
	return s.admitWorkbenchTaskOperation(input, connectionID, fingerprint, nil)
}

func (s *Store) AdmitWorkbenchConnectorOperation(input common.WorkbenchTaskWrite, connectionID, fingerprint string, binding common.WorkbenchConnectorOperationBinding) (common.WorkbenchTaskOperation, bool, error) {
	return s.admitWorkbenchTaskOperation(input, connectionID, fingerprint, &binding)
}

func (s *Store) admitWorkbenchTaskOperation(input common.WorkbenchTaskWrite, connectionID, fingerprint string, managed *common.WorkbenchConnectorOperationBinding) (common.WorkbenchTaskOperation, bool, error) {
	var empty common.WorkbenchTaskOperation
	tx, err := s.db.Begin()
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	existing, err := lookupWorkbenchTaskOperation(tx, input.RequestID)
	if err != nil {
		return empty, false, err
	}
	if existing.Fingerprint != "" {
		if existing.Fingerprint != fingerprint {
			return empty, false, common.Err("CONFLICT", "Request ID was already used with different input")
		}
		return existing, false, nil
	}
	var version int64
	var rawConfig string
	if err := tx.QueryRow("SELECT version,config FROM workbench_connections WHERE id=?", connectionID).Scan(&version, &rawConfig); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return empty, false, common.Err("NOT_FOUND", "Unknown workbench connection")
		}
		return empty, false, err
	}
	if version != input.ConnectionVersion {
		return empty, false, common.Err("STALE_REVISION", "Connection configuration changed")
	}
	var connection common.WorkbenchConnection
	if err := json.Unmarshal([]byte(rawConfig), &connection); err != nil {
		return empty, false, err
	}
	if connection.Paused || ((connection.Kind == common.WorkbenchObsidianTasks || connection.Kind == common.WorkbenchGitHubRepository) && !connection.WriteEnabled) {
		return empty, false, common.Err("CONFLICT", "Connection is not writable")
	}
	if connection.Kind == common.WorkbenchGitHubRepository {
		var access string
		var denied bool
		if err := tx.QueryRow("SELECT access_state,write_denied FROM workbench_github_connections WHERE connection_id=?", connectionID).Scan(&access, &denied); err != nil {
			return empty, false, common.Err("CONFLICT", "GitHub authorization is unavailable")
		}
		if access != "available" || denied {
			return empty, false, common.Err("CONFLICT", "GitHub authorization requires synchronization")
		}
	}
	var enabled bool
	if err := tx.QueryRow("SELECT COALESCE((SELECT enabled FROM workbench_plugin_settings WHERE id=?),1)", connection.Kind).Scan(&enabled); err != nil {
		return empty, false, err
	}
	if !enabled {
		return empty, false, common.Err("CONFLICT", "Connector plugin is disabled")
	}
	var baseline *common.WorkbenchTask
	if input.SourceObjectID != "" {
		var payload string
		if err := tx.QueryRow("SELECT payload FROM workbench_objects WHERE id=? AND connection_id=?", input.SourceObjectID, connectionID).Scan(&payload); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return empty, false, common.Err("NOT_FOUND", "Task is not visible in this connection")
			}
			return empty, false, err
		}
		baseline = &common.WorkbenchTask{}
		if err := json.Unmarshal([]byte(payload), baseline); err != nil {
			return empty, false, err
		}
	}
	receipt := common.WorkbenchTaskReceipt{RequestID: input.RequestID, ConnectionID: connectionID, SourceObjectID: input.SourceObjectID, SyncState: "accepted"}
	binding := &common.WorkbenchOperationBinding{Vault: connection.Vault, Scope: connection.Scope, Baseline: baseline, Connector: managed}
	if connection.Kind == common.WorkbenchGitHubRepository {
		if err := tx.QueryRow("SELECT repository_id,account_id FROM workbench_github_connections WHERE connection_id=?", connectionID).Scan(&binding.RepositoryID, &binding.AccountID); err != nil {
			return empty, false, err
		}
		if baseline != nil {
			issueID, idErr := strconv.ParseInt(baseline.NativeFields["issue_id"], 10, 64)
			number, numberErr := strconv.ParseInt(baseline.NativeID, 10, 64)
			if idErr != nil || numberErr != nil || issueID <= 0 || number <= 0 {
				return empty, false, common.Err("CONFLICT", "GitHub issue identity is unavailable")
			}
			binding.IssueID, binding.IssueNumber = issueID, number
		}
	}
	stored := workbenchStoredOperationInput{Request: input, Binding: binding}
	if _, err := tx.Exec(`INSERT INTO workbench_task_writes(request_id,connection_id,fingerprint,receipt,input,phase)
VALUES(?,?,?,?,?,'accepted')`, input.RequestID, connectionID, fingerprint, encode(receipt), encode(stored)); err != nil {
		return empty, false, err
	}
	if err := tx.Commit(); err != nil {
		return empty, false, err
	}
	return common.WorkbenchTaskOperation{Input: input, Receipt: receipt, Fingerprint: fingerprint, Phase: "accepted", Binding: binding}, true, nil
}

func lookupWorkbenchTaskOperation(tx *sql.Tx, requestID string) (common.WorkbenchTaskOperation, error) {
	var op common.WorkbenchTaskOperation
	var rawInput sql.NullString
	var rawReceipt string
	err := tx.QueryRow("SELECT fingerprint,receipt,input,phase FROM workbench_task_writes WHERE request_id=?", requestID).
		Scan(&op.Fingerprint, &rawReceipt, &rawInput, &op.Phase)
	if errors.Is(err, sql.ErrNoRows) {
		return op, nil
	}
	if err != nil {
		return op, err
	}
	if err := json.Unmarshal([]byte(rawReceipt), &op.Receipt); err != nil {
		return op, err
	}
	if rawInput.Valid {
		if err := decodeWorkbenchOperationInput(rawInput.String, &op); err != nil {
			return op, err
		}
	}
	return op, nil
}

// WorkbenchTaskOperation returns the original durable request, if it exists.
func (s *Store) WorkbenchTaskOperation(requestID string) (common.WorkbenchTaskOperation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return common.WorkbenchTaskOperation{}, err
	}
	defer tx.Rollback()
	op, err := lookupWorkbenchTaskOperation(tx, requestID)
	if err != nil {
		return op, err
	}
	if op.Fingerprint == "" {
		return op, common.Err("NOT_FOUND", "Unknown task operation")
	}
	return op, nil
}

// RestoreLegacyWorkbenchTaskOperation records the exact replayed request for a
// legacy uncertain receipt. It never dispatches the original write again.
func (s *Store) RestoreLegacyWorkbenchTaskOperation(input common.WorkbenchTaskWrite, fingerprint string) (common.WorkbenchTaskOperation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return common.WorkbenchTaskOperation{}, err
	}
	defer tx.Rollback()
	op, err := lookupWorkbenchTaskOperation(tx, input.RequestID)
	if err != nil {
		return op, err
	}
	if op.Fingerprint == "" {
		return op, common.Err("NOT_FOUND", "Unknown task operation")
	}
	if op.Fingerprint != fingerprint {
		return op, common.Err("CONFLICT", "Request ID was already used with different input")
	}
	if op.Phase != "legacy" || op.Receipt.SyncState != common.WorkbenchOperationNeedsReview {
		return op, nil
	}
	var rawConfig string
	var version int64
	if err := tx.QueryRow("SELECT config,version FROM workbench_connections WHERE id=?", op.Receipt.ConnectionID).Scan(&rawConfig, &version); err != nil {
		return op, err
	}
	var connection common.WorkbenchConnection
	if err := json.Unmarshal([]byte(rawConfig), &connection); err != nil {
		return op, err
	}
	var binding *common.WorkbenchOperationBinding
	if version == input.ConnectionVersion {
		binding = &common.WorkbenchOperationBinding{Vault: connection.Vault, Scope: connection.Scope}
	}
	stored := workbenchStoredOperationInput{Request: input, Binding: binding}
	result, err := tx.Exec("UPDATE workbench_task_writes SET input=?,phase='needs_review' WHERE request_id=? AND phase='legacy' AND fingerprint=?", encode(stored), input.RequestID, fingerprint)
	if err != nil {
		return op, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return op, err
	}
	if count != 1 {
		return op, common.Err("STALE_REVISION", "Task operation changed")
	}
	if err := tx.Commit(); err != nil {
		return op, err
	}
	op.Input, op.Phase, op.Binding = input, common.WorkbenchOperationNeedsReview, binding
	return op, nil
}

// PendingWorkbenchTaskOperations returns recoverable requests in acceptance order.
func (s *Store) PendingWorkbenchTaskOperations() ([]common.WorkbenchTaskOperation, error) {
	rows, err := s.db.Query(`SELECT fingerprint,receipt,input,phase FROM workbench_task_writes
WHERE phase IN ('accepted','dispatching','needs_review') AND input IS NOT NULL ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []common.WorkbenchTaskOperation
	for rows.Next() {
		var op common.WorkbenchTaskOperation
		var input, receipt string
		if err := rows.Scan(&op.Fingerprint, &receipt, &input, &op.Phase); err != nil {
			return nil, err
		}
		if err := decodeWorkbenchOperationInput(input, &op); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(receipt), &op.Receipt); err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, rows.Err()
}

// RecoverWorkbenchTaskOperations preserves uncertainty after a process exits
// while a source write might have been dispatched.
func (s *Store) RecoverWorkbenchTaskOperations() error {
	_, err := s.db.Exec(`UPDATE workbench_task_writes
SET phase='needs_review', receipt=json_set(receipt, '$.sync_state', 'needs_review', '$.reason', 'The application stopped while this operation may have been dispatched')
WHERE phase='dispatching' AND input IS NOT NULL`)
	return err
}

// AdvanceWorkbenchTaskOperation atomically compares the processing stage before advancing it.
func (s *Store) AdvanceWorkbenchTaskOperation(requestID, from, to string, receipt common.WorkbenchTaskReceipt) error {
	allowed := map[string]map[string]bool{
		"accepted":     {"dispatching": true, "needs_review": true, "conflict": true, "not_executed": true},
		"dispatching":  {"confirmed": true, "needs_review": true, "conflict": true, "not_executed": true},
		"needs_review": {"confirmed": true, "conflict": true, "not_executed": true},
	}
	if !allowed[from][to] || receipt.RequestID != requestID || receipt.SyncState != to {
		return common.Err("INVALID_ARGUMENT", "Invalid task operation transition")
	}
	result, err := s.db.Exec(`UPDATE workbench_task_writes SET phase=?,receipt=?
WHERE request_id=? AND connection_id=? AND phase=?`, to, encode(receipt), requestID, receipt.ConnectionID, from)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return common.Err("STALE_REVISION", "Task operation changed")
	}
	return nil
}

// ClaimWorkbenchTaskOperation fences older observations in the same transaction
// that marks a request as potentially dispatched.
func (s *Store) ClaimWorkbenchTaskOperation(requestID string) (common.WorkbenchTaskOperation, error) {
	return s.claimWorkbenchTaskOperation(requestID, nil)
}

func (s *Store) ClaimWorkbenchTaskOperationIfSnapshot(requestID string, expected int64) (common.WorkbenchTaskOperation, error) {
	return s.claimWorkbenchTaskOperation(requestID, &expected)
}

func (s *Store) claimWorkbenchTaskOperation(requestID string, expected *int64) (common.WorkbenchTaskOperation, error) {
	var empty common.WorkbenchTaskOperation
	tx, err := s.db.Begin()
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	op, err := lookupWorkbenchTaskOperation(tx, requestID)
	if err != nil {
		return empty, err
	}
	if op.Fingerprint == "" {
		return empty, common.Err("NOT_FOUND", "Unknown task operation")
	}
	if op.Phase != "accepted" {
		return empty, common.Err("STALE_REVISION", "Task operation is no longer awaiting dispatch")
	}
	var config string
	var version, snapshotVersion int64
	if err := tx.QueryRow("SELECT config,version,snapshot_version FROM workbench_connections WHERE id=?", op.Receipt.ConnectionID).Scan(&config, &version, &snapshotVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return empty, common.Err("NOT_FOUND", "Unknown workbench connection")
		}
		return empty, err
	}
	if expected != nil && snapshotVersion != *expected {
		return empty, common.Err("STALE_REVISION", "Source snapshot changed before dispatch")
	}
	var connection common.WorkbenchConnection
	if err := json.Unmarshal([]byte(config), &connection); err != nil {
		return empty, err
	}
	if version != op.Input.ConnectionVersion || connection.Paused || ((connection.Kind == common.WorkbenchObsidianTasks || connection.Kind == common.WorkbenchGitHubRepository) && !connection.WriteEnabled) {
		return empty, common.Err("CONFLICT", "Connection is no longer writable with this configuration")
	}
	if connection.Kind == common.WorkbenchGitHubRepository {
		var repoID, accountID int64
		var access string
		var denied bool
		if err := tx.QueryRow("SELECT repository_id,account_id,access_state,write_denied FROM workbench_github_connections WHERE connection_id=?", connection.ID).Scan(&repoID, &accountID, &access, &denied); err != nil {
			return empty, common.Err("CONFLICT", "GitHub authorization is unavailable")
		}
		if op.Binding == nil || op.Binding.RepositoryID != repoID || op.Binding.AccountID != accountID || access != "available" || denied {
			return empty, common.Err("CONFLICT", "GitHub authorization changed before dispatch")
		}
	}
	var enabled bool
	if err := tx.QueryRow("SELECT COALESCE((SELECT enabled FROM workbench_plugin_settings WHERE id=?),1)", connection.Kind).Scan(&enabled); err != nil {
		return empty, err
	}
	if !enabled {
		return empty, common.Err("CONFLICT", "Connector plugin is disabled")
	}
	if op.Input.SourceObjectID != "" {
		var exists int
		if err := tx.QueryRow("SELECT 1 FROM workbench_objects WHERE id=? AND connection_id=?", op.Input.SourceObjectID, connection.ID).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return empty, common.Err("CONFLICT", "Task is no longer visible in this connection")
			}
			return empty, err
		}
	}
	op.Phase, op.Receipt.SyncState = "dispatching", "dispatching"
	result, err := tx.Exec("UPDATE workbench_task_writes SET phase=?,receipt=? WHERE request_id=? AND phase='accepted'", op.Phase, encode(op.Receipt), requestID)
	if err != nil {
		return empty, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return empty, err
	}
	if changed != 1 {
		return empty, common.Err("STALE_REVISION", "Task operation changed")
	}
	if _, err := tx.Exec("UPDATE workbench_connections SET snapshot_version=snapshot_version+1 WHERE id=? AND version=?", connection.ID, version); err != nil {
		return empty, err
	}
	if err := tx.Commit(); err != nil {
		return empty, err
	}
	return op, nil
}

// CommitWorkbenchTaskOperation publishes a verified snapshot and settles its
// operation together, provided the observation has not been superseded.
func (s *Store) CommitWorkbenchTaskOperation(connection common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, now time.Time, expectedVersion int64, receipt common.WorkbenchTaskReceipt) error {
	return s.commitWorkbenchTaskOperation(connection, attempts, before, after, now, expectedVersion, receipt, true)
}

// CommitWorkbenchTaskOperationPartial keeps the last full confirmation time
// when some unrelated source identities remain ambiguous.
func (s *Store) CommitWorkbenchTaskOperationPartial(connection common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, now time.Time, expectedVersion int64, receipt common.WorkbenchTaskReceipt) error {
	return s.commitWorkbenchTaskOperation(connection, attempts, before, after, now, expectedVersion, receipt, false)
}

func (s *Store) commitWorkbenchTaskOperation(connection common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, now time.Time, expectedVersion int64, receipt common.WorkbenchTaskReceipt, full bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int64
	var currentAttempts string
	if err := tx.QueryRow(`SELECT snapshot_version,attempts FROM workbench_connections WHERE id=? AND version=?
AND COALESCE((SELECT enabled FROM workbench_plugin_settings WHERE id=json_extract(config,'$.kind')),1)=1`, connection.ID, connection.Version).Scan(&version, &currentAttempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Err("STALE_REVISION", "Connection configuration changed")
		}
		return err
	}
	if version != expectedVersion {
		return common.Err("STALE_REVISION", "Source snapshot changed")
	}
	if receipt.SyncState != "confirmed" || receipt.ConnectionID != connection.ID {
		return common.Err("INVALID_ARGUMENT", "Invalid confirmed task operation")
	}
	result, err := tx.Exec(`UPDATE workbench_task_writes SET phase='confirmed',receipt=?
WHERE request_id=? AND connection_id=? AND phase IN ('dispatching','needs_review')`, encode(receipt), receipt.RequestID, connection.ID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return common.Err("STALE_REVISION", "Task operation changed")
	}
	// A scan can start without advancing the snapshot version. Keep its
	// in-flight attempt instead of replacing it with stale history from the read.
	var latest []common.WorkbenchAttempt
	if err := json.Unmarshal([]byte(currentAttempts), &latest); err != nil {
		return err
	}
	if len(attempts) > 0 {
		if len(latest) > 0 && latest[0].State == "running" {
			latest = append(latest[:1], append([]common.WorkbenchAttempt{attempts[0]}, latest[1:]...)...)
		} else {
			latest = append([]common.WorkbenchAttempt{attempts[0]}, latest...)
		}
	}
	attempts = latest[:min(20, len(latest))]
	var success *time.Time
	if full {
		success = &now
	}
	if err := recordWorkbenchSnapshot(tx, connection, attempts, before, after, success, &now); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitWorkbenchObservedOperation settles one observed task and its
// receipt atomically without advancing the full-scope confirmation time.
func (s *Store) CommitWorkbenchObservedOperation(connection common.WorkbenchConnection, version int64, task common.WorkbenchTask, attempt common.WorkbenchAttempt, receipt common.WorkbenchTaskReceipt) error {
	if receipt.SyncState != common.WorkbenchOperationConfirmed || receipt.ConnectionID != connection.ID || receipt.SourceObjectID != task.ID {
		return common.Err("INVALID_ARGUMENT", "Invalid single-note confirmation")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	op, err := lookupWorkbenchTaskOperation(tx, receipt.RequestID)
	if err != nil {
		return err
	}
	if op.Phase != common.WorkbenchOperationNeedsReview || op.Receipt.ConnectionID != connection.ID || op.Input.SourceObjectID != task.ID || op.Input.State != task.State {
		return common.Err("STALE_REVISION", "Task operation changed")
	}
	var rawTask string
	if err := tx.QueryRow("SELECT payload FROM workbench_objects WHERE id=? AND connection_id=?", task.ID, connection.ID).Scan(&rawTask); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Err("STALE_REVISION", "Task mapping changed")
		}
		return err
	}
	var previous common.WorkbenchTask
	if err := json.Unmarshal([]byte(rawTask), &previous); err != nil {
		return err
	}
	if previous.NativeID != task.NativeID {
		return common.Err("STALE_REVISION", "Task source identity changed")
	}
	if err := commitWorkbenchObservedTasks(tx, connection, version, []common.WorkbenchTask{task}, true); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE workbench_task_writes SET phase='confirmed',receipt=? WHERE request_id=? AND connection_id=? AND phase='needs_review'`, encode(receipt), receipt.RequestID, connection.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return common.Err("STALE_REVISION", "Task operation changed")
	}
	var raw string
	if err := tx.QueryRow("SELECT attempts FROM workbench_connections WHERE id=?", connection.ID).Scan(&raw); err != nil {
		return err
	}
	var attempts []common.WorkbenchAttempt
	if err := json.Unmarshal([]byte(raw), &attempts); err != nil {
		return err
	}
	if len(attempts) > 0 && attempts[0].State == "running" {
		attempts = append(attempts[:1], append([]common.WorkbenchAttempt{attempt}, attempts[1:]...)...)
	} else {
		attempts = append([]common.WorkbenchAttempt{attempt}, attempts...)
	}
	if _, err := tx.Exec("UPDATE workbench_connections SET attempts=? WHERE id=?", encode(attempts[:min(20, len(attempts))]), connection.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitWorkbenchObservedCreation publishes one uniquely observed created task
// and its receipt without claiming a full-source verification.
func (s *Store) CommitWorkbenchObservedCreation(connection common.WorkbenchConnection, version int64, task common.WorkbenchTask, attempt common.WorkbenchAttempt, receipt common.WorkbenchTaskReceipt) error {
	if receipt.SyncState != common.WorkbenchOperationConfirmed || receipt.ConnectionID != connection.ID || receipt.SourceObjectID != task.ID || task.ID == "" {
		return common.Err("INVALID_ARGUMENT", "Invalid single-note creation confirmation")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	op, err := lookupWorkbenchTaskOperation(tx, receipt.RequestID)
	if err != nil {
		return err
	}
	if op.Phase != common.WorkbenchOperationNeedsReview || op.Receipt.ConnectionID != connection.ID || op.Input.SourceObjectID != "" || len(op.Fingerprint) < 32 || task.NativeID != "apilot-"+op.Fingerprint[:32] || task.NativeFields["path"] != op.Input.NotePath {
		return common.Err("STALE_REVISION", "Task operation or source identity changed")
	}
	heading, err := json.Marshal(op.Input.HeadingPath)
	if err != nil || task.NativeFields["heading_path"] != string(heading) {
		return common.Err("STALE_REVISION", "Task heading changed")
	}
	var currentVersion int64
	var rawAttempts string
	if err := tx.QueryRow(`SELECT snapshot_version,attempts FROM workbench_connections WHERE id=? AND version=?
AND COALESCE((SELECT enabled FROM workbench_plugin_settings WHERE id=json_extract(config,'$.kind')),1)=1`, connection.ID, connection.Version).Scan(&currentVersion, &rawAttempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.Err("STALE_REVISION", "Connection configuration changed")
		}
		return err
	}
	if currentVersion != version {
		return common.Err("STALE_REVISION", "Source snapshot changed")
	}
	var previous []common.WorkbenchTask
	var owner, key, payload string
	err = tx.QueryRow("SELECT connection_id,source_key,payload FROM workbench_objects WHERE id=?", task.ID).Scan(&owner, &key, &payload)
	if err == nil {
		var prior common.WorkbenchTask
		if err := json.Unmarshal([]byte(payload), &prior); err != nil {
			return err
		}
		if owner != connection.ID || key != task.SourceKey || prior.NativeID != task.NativeID {
			return common.Err("STALE_REVISION", "Task mapping changed")
		}
		prior.SourceKey = key
		previous = append(previous, prior)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var attempts []common.WorkbenchAttempt
	if err := json.Unmarshal([]byte(rawAttempts), &attempts); err != nil {
		return err
	}
	if len(attempts) > 0 && attempts[0].State == "running" {
		attempts = append(attempts[:1], append([]common.WorkbenchAttempt{attempt}, attempts[1:]...)...)
	} else {
		attempts = append([]common.WorkbenchAttempt{attempt}, attempts...)
	}
	result, err := tx.Exec("UPDATE workbench_task_writes SET phase='confirmed',receipt=? WHERE request_id=? AND connection_id=? AND phase='needs_review'", encode(receipt), receipt.RequestID, connection.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return common.Err("STALE_REVISION", "Task operation changed")
	}
	now := attempt.Started
	if err := recordWorkbenchSnapshot(tx, connection, attempts[:min(20, len(attempts))], previous, []common.WorkbenchTask{task}, nil, &now); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordWorkbenchGitHubEvidence saves the source identity before attempting read-back.
// It never moves a request back to a dispatchable state.
func (s *Store) RecordWorkbenchGitHubEvidence(requestID string, issueID, issueNumber int64) error {
	if issueID <= 0 || issueNumber <= 0 {
		return common.Err("INVALID_ARGUMENT", "GitHub issue identity is required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	op, err := lookupWorkbenchTaskOperation(tx, requestID)
	if err != nil {
		return err
	}
	if op.Fingerprint == "" || op.Phase != common.WorkbenchOperationDispatching || op.Binding == nil || op.Binding.RepositoryID == 0 {
		return common.Err("STALE_REVISION", "GitHub operation is not awaiting evidence")
	}
	if op.Binding.IssueID != 0 && (op.Binding.IssueID != issueID || op.Binding.IssueNumber != issueNumber) {
		return common.Err("CONFLICT", "GitHub issue identity changed")
	}
	op.Binding.IssueID, op.Binding.IssueNumber = issueID, issueNumber
	input := workbenchStoredOperationInput{Request: op.Input, Binding: op.Binding}
	result, err := tx.Exec("UPDATE workbench_task_writes SET input=? WHERE request_id=? AND phase='dispatching'", encode(input), requestID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return common.Err("STALE_REVISION", "GitHub operation changed")
	}
	return tx.Commit()
}
