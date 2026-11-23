package dal

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func (s *Store) WorkbenchGitHubAccountBackoffs() (map[int64]time.Time, error) {
	rows, err := s.db.Query("SELECT account_id,blocked_until FROM workbench_github_account_backoffs WHERE julianday(blocked_until)>julianday('now')")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64]time.Time)
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			return nil, err
		}
		until, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, err
		}
		result[id] = until
	}
	return result, rows.Err()
}

func (s *Store) RecordWorkbenchGitHubAccountBackoff(accountID int64, until time.Time) error {
	if accountID <= 0 {
		return common.Err("INVALID_ARGUMENT", "Invalid GitHub account")
	}
	value := until.UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`INSERT INTO workbench_github_account_backoffs(account_id,blocked_until) VALUES(?,?)
		ON CONFLICT(account_id) DO UPDATE SET blocked_until=CASE WHEN julianday(excluded.blocked_until)>julianday(blocked_until) THEN excluded.blocked_until ELSE blocked_until END`, accountID, value)
	return err
}

// WorkbenchGitHubCredentialRefs returns only references, never credential values.
func (s *Store) WorkbenchGitHubCredentialRefs() ([]string, error) {
	rows, err := s.db.Query("SELECT credential_ref FROM workbench_github_connections")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

// WorkbenchGitHubCredentialAccounts groups stored credentials by verified user.
func (s *Store) WorkbenchGitHubCredentialAccounts() (map[string]int64, error) {
	rows, err := s.db.Query("SELECT DISTINCT credential_ref, account_id FROM workbench_github_connections")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var ref string
		var account int64
		if err := rows.Scan(&ref, &account); err != nil {
			return nil, err
		}
		if prior, found := result[ref]; found && prior != account {
			return nil, common.Err("INTERNAL", "GitHub credential is associated with different accounts")
		}
		result[ref] = account
	}
	return result, rows.Err()
}

func (s *Store) WorkbenchGitHubProgress(id string) (common.WorkbenchGitHubProgress, error) {
	return s.workbenchGitHubProgress("WHERE connection_id=?", id)
}

// WorkbenchGitHubProgressAt rejects a mixed connection/authorization read.
func (s *Store) WorkbenchGitHubProgressAt(id string, version, snapshotVersion int64) (common.WorkbenchGitHubProgress, error) {
	progress, err := s.workbenchGitHubProgress(`WHERE connection_id=? AND EXISTS (
		SELECT 1 FROM workbench_connections c WHERE c.id=connection_id AND c.version=? AND c.snapshot_version=?
	)`, id, version, snapshotVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return progress, common.Err("STALE_REVISION", "GitHub connection changed during read")
	}
	return progress, err
}

func (s *Store) workbenchGitHubProgress(where string, args ...any) (common.WorkbenchGitHubProgress, error) {
	var progress common.WorkbenchGitHubProgress
	var cursor, full, nextPoll sql.NullString
	err := s.db.QueryRow(`SELECT connection_id,repository_id,account_id,account_login,credential_ref,access_state,cursor,last_full_sync,write_denied,next_poll_at
		FROM workbench_github_connections `+where, args...).Scan(
		&progress.ConnectionID, &progress.RepositoryID, &progress.AccountID, &progress.AccountLogin,
		&progress.CredentialRef, &progress.AccessState, &cursor, &full, &progress.WriteDenied, &nextPoll)
	if err != nil {
		return progress, err
	}
	if cursor.Valid {
		value, err := time.Parse(time.RFC3339Nano, cursor.String)
		if err != nil {
			return progress, err
		}
		progress.Cursor = &value
	}
	if full.Valid {
		value, err := time.Parse(time.RFC3339Nano, full.String)
		if err != nil {
			return progress, err
		}
		progress.LastFullSync = &value
	}
	if nextPoll.Valid {
		value, err := time.Parse(time.RFC3339Nano, nextPoll.String)
		if err != nil {
			return progress, err
		}
		progress.NextPollAt = &value
	}
	return progress, nil
}

// RecordWorkbenchGitHubPoll persists server guidance even when a later page fails.
func (s *Store) RecordWorkbenchGitHubPoll(c common.WorkbenchConnection, accountID int64, until time.Time) error {
	value := until.UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`UPDATE workbench_github_connections SET next_poll_at=CASE
		WHEN next_poll_at IS NULL OR julianday(next_poll_at)<julianday(?) THEN ? ELSE next_poll_at END
		WHERE connection_id=? AND account_id=? AND EXISTS (SELECT 1 FROM workbench_connections c WHERE c.id=connection_id AND c.version=?)`, value, value, c.ID, accountID, c.Version)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return common.Err("STALE_REVISION", "Connection authorization changed")
	}
	return nil
}

// RejectWorkbenchGitHubWrite records an explicit upstream rejection and only
// disables the authorization that actually produced it.
func (s *Store) RejectWorkbenchGitHubWrite(requestID string, receipt common.WorkbenchTaskReceipt, unauthorized bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	op, err := lookupWorkbenchTaskOperation(tx, requestID)
	if err != nil {
		return err
	}
	if op.Phase != common.WorkbenchOperationDispatching || op.Binding == nil || receipt.RequestID != requestID || receipt.ConnectionID != op.Receipt.ConnectionID || receipt.SyncState != common.WorkbenchOperationNotExecuted {
		return common.Err("STALE_REVISION", "Task operation changed")
	}
	result, err := tx.Exec("UPDATE workbench_task_writes SET phase=?,receipt=? WHERE request_id=? AND phase='dispatching'", receipt.SyncState, encode(receipt), requestID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return err
		}
		return common.Err("STALE_REVISION", "Task operation changed")
	}
	result, err = tx.Exec(`UPDATE workbench_github_connections SET write_denied=1 WHERE connection_id=? AND repository_id=? AND account_id=?
		AND EXISTS (SELECT 1 FROM workbench_connections c WHERE c.id=connection_id AND c.version=?)`, receipt.ConnectionID, op.Binding.RepositoryID, op.Binding.AccountID, op.Input.ConnectionVersion)
	if err != nil {
		return err
	}
	if unauthorized {
		matched, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if matched == 1 {
			if _, err := tx.Exec(`UPDATE workbench_github_connections SET access_state='unavailable',cursor=NULL,last_full_sync=NULL WHERE connection_id=?`, receipt.ConnectionID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE workbench_connections SET snapshot_version=snapshot_version+1,last_success=NULL,attempts='[]' WHERE id=?`, receipt.ConnectionID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE workbench_revision SET revision=revision+1`); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// InvalidateWorkbenchGitHubAccess hides a revoked snapshot and fences any
// observation started before the authorization failure was established.
func (s *Store) InvalidateWorkbenchGitHubAccess(c common.WorkbenchConnection, snapshotVersion int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE workbench_connections SET snapshot_version=snapshot_version+1,last_success=NULL,attempts='[]'
		WHERE id=? AND version=? AND snapshot_version=?`, c.ID, c.Version, snapshotVersion)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return common.Err("STALE_REVISION", "Source snapshot or connection changed")
	}
	if _, err := tx.Exec(`UPDATE workbench_github_connections SET access_state='unavailable',cursor=NULL,last_full_sync=NULL WHERE connection_id=?`, c.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE workbench_revision SET revision=revision+1`); err != nil {
		return err
	}
	return tx.Commit()
}

// CommitWorkbenchGitHubSync publishes the snapshot and its cursor in one transaction.
func (s *Store) CommitWorkbenchGitHubSync(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, observed time.Time, snapshotVersion int64, cursor time.Time, full bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentSnapshot, currentConnection int64
	if err := tx.QueryRow("SELECT snapshot_version,version FROM workbench_connections WHERE id=?", c.ID).Scan(&currentSnapshot, &currentConnection); err != nil {
		return err
	}
	if currentSnapshot != snapshotVersion || currentConnection != c.Version {
		return common.Err("STALE_REVISION", "Source snapshot or connection changed")
	}
	var previous sql.NullString
	var accessState string
	if err := tx.QueryRow("SELECT cursor,access_state FROM workbench_github_connections WHERE connection_id=?", c.ID).Scan(&previous, &accessState); err != nil {
		return err
	}
	if previous.Valid {
		last, err := time.Parse(time.RFC3339Nano, previous.String)
		if err != nil {
			return err
		}
		if cursor.Before(last) {
			return common.Err("STALE_REVISION", "GitHub cursor would move backwards")
		}
	}
	if err := recordWorkbench(tx, c, attempts, before, after, &observed); err != nil {
		return err
	}
	cursorValue := cursor.UTC().Format(time.RFC3339Nano)
	if full {
		_, err = tx.Exec("UPDATE workbench_github_connections SET cursor=?,last_full_sync=?,access_state='available' WHERE connection_id=?", cursorValue, observed.UTC().Format(time.RFC3339Nano), c.ID)
	} else {
		_, err = tx.Exec("UPDATE workbench_github_connections SET cursor=? WHERE connection_id=?", cursorValue, c.ID)
	}
	if err != nil {
		return err
	}
	if full && accessState != "available" {
		if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SaveWorkbenchGitHub saves a verified repository binding with an optimistic
// connection version. The credential reference is stored separately from its value.
func (s *Store) SaveWorkbenchGitHub(c common.WorkbenchConnection, expectedVersion int64, progress common.WorkbenchGitHubProgress) error {
	if c.ID == "" || c.Kind != common.WorkbenchGitHubRepository || progress.ConnectionID != c.ID ||
		progress.RepositoryID <= 0 || progress.AccountID <= 0 || progress.CredentialRef == "" {
		return common.Err("INVALID_ARGUMENT", "Invalid GitHub connection")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingVersion int64
	err = tx.QueryRow("SELECT version FROM workbench_connections WHERE id=?", c.ID).Scan(&existingVersion)
	if errors.Is(err, sql.ErrNoRows) {
		if expectedVersion != 0 || c.Version != 1 {
			return common.Err("STALE_REVISION", "Connection no longer exists")
		}
		if _, err := tx.Exec("INSERT INTO workbench_connections(id,config,version) VALUES(?,?,?)", c.ID, encode(c), c.Version); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		if expectedVersion != existingVersion || c.Version != existingVersion+1 {
			return common.Err("STALE_REVISION", "Connection configuration changed")
		}
		var repoID, accountID int64
		var previousAccess string
		if err := tx.QueryRow("SELECT repository_id,account_id,access_state FROM workbench_github_connections WHERE connection_id=?", c.ID).Scan(&repoID, &accountID, &previousAccess); err != nil {
			return err
		}
		if repoID != progress.RepositoryID {
			return common.Err("CONFLICT", "Connection belongs to a different repository")
		}
		if accountID != progress.AccountID {
			progress.AccessState = "pending"
			if _, err := tx.Exec("UPDATE workbench_github_connections SET cursor=NULL,last_full_sync=NULL,next_poll_at=NULL WHERE connection_id=?", c.ID); err != nil {
				return err
			}
			if _, err := tx.Exec("UPDATE workbench_connections SET snapshot_version=snapshot_version+1,last_success=NULL,attempts='[]' WHERE id=?", c.ID); err != nil {
				return err
			}
		}
		if accountID != progress.AccountID || previousAccess != progress.AccessState {
			if _, err := tx.Exec("UPDATE workbench_revision SET revision=revision+1"); err != nil {
				return err
			}
		}
		if _, err := tx.Exec("UPDATE workbench_connections SET config=?,version=? WHERE id=?", encode(c), c.Version, c.ID); err != nil {
			return err
		}
	}
	var existingID string
	err = tx.QueryRow("SELECT connection_id FROM workbench_github_connections WHERE repository_id=? AND connection_id<>?", progress.RepositoryID, c.ID).Scan(&existingID)
	if err == nil {
		problem := common.Err("CONFLICT", "Repository is already connected")
		problem.Details["connection_id"] = existingID
		return problem
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(`INSERT INTO workbench_github_connections(connection_id,repository_id,account_id,account_login,credential_ref,access_state)
		VALUES(?,?,?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET account_login=excluded.account_login,
		account_id=excluded.account_id,credential_ref=excluded.credential_ref,access_state=excluded.access_state,write_denied=0`,
		c.ID, progress.RepositoryID, progress.AccountID, progress.AccountLogin, progress.CredentialRef, progress.AccessState)
	if err != nil {
		return err
	}
	return tx.Commit()
}
