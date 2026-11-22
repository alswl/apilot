package services

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type githubOperationStore interface {
	workbenchTaskOperationStore
	ClaimWorkbenchTaskOperationIfSnapshot(string, int64) (common.WorkbenchTaskOperation, error)
	RecordWorkbenchGitHubEvidence(string, int64, int64) error
	RejectWorkbenchGitHubWrite(string, common.WorkbenchTaskReceipt, bool) error
	CommitWorkbenchTaskOperationPartial(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, time.Time, int64, common.WorkbenchTaskReceipt) error
}

func (s *Service) githubOperationClient(ctx context.Context, op common.WorkbenchTaskOperation) (common.WorkbenchSnapshot, *integrations.WorkbenchGitHubClient, func(), error) {
	connection, err := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
	if err != nil {
		return connection, nil, nil, err
	}
	store, ok := s.store.(workbenchGitHubStore)
	if !ok {
		return connection, nil, nil, common.Err("INTERNAL", "GitHub connection storage unavailable")
	}
	progress, err := store.WorkbenchGitHubProgress(connection.ID)
	if err != nil {
		return connection, nil, nil, err
	}
	if op.Binding == nil || op.Binding.RepositoryID != progress.RepositoryID || op.Binding.AccountID != progress.AccountID || progress.AccessState != "available" {
		return connection, nil, nil, common.Err("CONFLICT", "GitHub authorization changed")
	}
	credentials, err := s.workbenchGitHubCredentials()
	if err != nil {
		return connection, nil, nil, err
	}
	token, release, err := s.acquireGitHubCredential(ctx, credentials, progress.CredentialRef)
	if err != nil {
		if invalidateErr := s.invalidateGitHubOperationAuthorization(connection, err); invalidateErr != nil {
			return connection, nil, nil, invalidateErr
		}
		return connection, nil, nil, err
	}
	client, err := s.workbenchGitHubClient(token)
	if err == nil {
		client.CoordinateRequests(s.githubRate, progress.AccountID)
	} else {
		release()
		return connection, nil, nil, err
	}
	credentialValue, err := credentials.Get(ctx, progress.CredentialRef)
	if err == nil {
		err = s.verifyGitHubCredentialInstallation(ctx, client, credentialValue, progress.RepositoryID)
	}
	if err != nil {
		release()
		if invalidateErr := s.invalidateGitHubOperationAuthorization(connection, err); invalidateErr != nil {
			return connection, nil, nil, invalidateErr
		}
		return connection, nil, nil, err
	}
	return connection, client, release, nil
}

func (s *Service) invalidateGitHubOperationAuthorization(connection common.WorkbenchSnapshot, err error) error {
	var problem *common.Error
	if !errors.As(err, &problem) || (problem.Code != "FORBIDDEN" && problem.Code != "UNAUTHORIZED") {
		return nil
	}
	if invalidator, ok := s.store.(interface {
		InvalidateWorkbenchGitHubAccess(common.WorkbenchConnection, int64) error
	}); ok {
		return invalidator.InvalidateWorkbenchGitHubAccess(connection.WorkbenchConnection, connection.SnapshotVersion)
	}
	return nil
}

func (s *Service) executeGitHubTaskOperation(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, error) {
	store, ok := s.store.(githubOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "GitHub operation storage unavailable")
	}
	op, err := store.WorkbenchTaskOperation(requestID)
	if err != nil || op.Phase != common.WorkbenchOperationAccepted {
		return op.Receipt, err
	}
	connection, client, releaseCredential, err := s.githubOperationClient(ctx, op)
	if err != nil {
		return op.Receipt, err
	}
	defer releaseCredential()
	var number int64
	if op.Input.SourceObjectID != "" {
		if op.Binding.Baseline == nil {
			return op.Receipt, common.Err("CONFLICT", "Task baseline unavailable")
		}
		if !workbenchTaskMatchesTarget(*op.Binding.Baseline, op.Input.TargetVersion) {
			receipt := op.Receipt
			receipt.SyncState, receipt.Reason = common.WorkbenchOperationConflict, "Task changed before dispatch"
			return receipt, store.AdvanceWorkbenchTaskOperation(requestID, common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, receipt)
		}
		number, err = strconv.ParseInt(op.Binding.Baseline.NativeID, 10, 64)
		if err != nil {
			return op.Receipt, common.Err("CONFLICT", "Invalid issue identity")
		}
	}
	writeCtx, release, err := client.AcquireWrite(ctx)
	if err != nil {
		return op.Receipt, err
	}
	repository, err := client.Repository(writeCtx, connection.Repository)
	if err != nil {
		release()
		var problem *common.Error
		invalidReason := err
		if errors.As(err, &problem) && problem.Code == "NOT_FOUND" {
			invalidReason = common.Err("FORBIDDEN", "GitHub repository is unavailable")
		}
		if invalidateErr := s.invalidateGitHubOperationAuthorization(connection, invalidReason); invalidateErr != nil {
			return op.Receipt, invalidateErr
		}
		return op.Receipt, err
	}
	if repository.ID != op.Binding.RepositoryID || !strings.EqualFold(repository.FullName, connection.Repository) || !repository.HasIssues {
		release()
		receipt := op.Receipt
		receipt.SyncState, receipt.Reason = common.WorkbenchOperationConflict, "GitHub repository changed before dispatch"
		return receipt, store.AdvanceWorkbenchTaskOperation(requestID, common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, receipt)
	}
	if number != 0 {
		current, err := client.Issue(writeCtx, connection.WorkbenchConnection, number)
		if err != nil {
			release()
			if invalidateErr := s.invalidateGitHubOperationAuthorization(connection, err); invalidateErr != nil {
				return op.Receipt, invalidateErr
			}
			return op.Receipt, err
		}
		if current.NativeFields["issue_id"] != op.Binding.Baseline.NativeFields["issue_id"] || current.NativeFields["updated_at"] != op.Binding.Baseline.NativeFields["updated_at"] {
			release()
			receipt := op.Receipt
			receipt.SyncState, receipt.Reason = common.WorkbenchOperationConflict, "GitHub issue changed before dispatch"
			return receipt, store.AdvanceWorkbenchTaskOperation(requestID, common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, receipt)
		}
	}
	if _, err := store.ClaimWorkbenchTaskOperationIfSnapshot(requestID, connection.SnapshotVersion); err != nil {
		release()
		return op.Receipt, err
	}
	written, writeErr := client.WriteIssue(writeCtx, connection.WorkbenchConnection, number, op.Input.Title, op.Input.Body, op.Input.State)
	release()
	var problem *common.Error
	if errors.As(writeErr, &problem) && (problem.Code == "FORBIDDEN" || problem.Code == "UNAUTHORIZED") {
		receipt := op.Receipt
		receipt.SyncState, receipt.Reason = common.WorkbenchOperationNotExecuted, problem.Message
		if err := store.RejectWorkbenchGitHubWrite(requestID, receipt, problem.Code == "UNAUTHORIZED"); err != nil {
			return op.Receipt, err
		}
		return receipt, nil
	}
	if errors.As(writeErr, &problem) && problem.Code == "RATE_LIMITED" {
		receipt := op.Receipt
		receipt.SyncState, receipt.Reason = common.WorkbenchOperationNotExecuted, problem.Message
		if err := store.AdvanceWorkbenchTaskOperation(requestID, common.WorkbenchOperationDispatching, common.WorkbenchOperationNotExecuted, receipt); err != nil {
			return op.Receipt, err
		}
		return receipt, nil
	}
	if writeErr == nil {
		id, idErr := strconv.ParseInt(written.NativeFields["issue_id"], 10, 64)
		num, numErr := strconv.ParseInt(written.NativeID, 10, 64)
		if number != 0 && written.NativeFields["issue_id"] != op.Binding.Baseline.NativeFields["issue_id"] {
			writeErr = common.Err("CONFLICT", "GitHub write returned a different issue identity")
		} else if idErr == nil && numErr == nil {
			writeErr = store.RecordWorkbenchGitHubEvidence(requestID, id, num)
		}
	}
	receipt := op.Receipt
	receipt.SyncState, receipt.Reason = common.WorkbenchOperationNeedsReview, "Waiting for GitHub source confirmation"
	if err := store.AdvanceWorkbenchTaskOperation(requestID, common.WorkbenchOperationDispatching, common.WorkbenchOperationNeedsReview, receipt); err != nil {
		return receipt, err
	}
	if writeErr != nil {
		return receipt, nil
	}
	releaseCredential()
	return s.reconcileGitHubTaskOperation(ctx, requestID)
}

func (s *Service) reconcileGitHubTaskOperation(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, error) {
	store, ok := s.store.(githubOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "GitHub operation storage unavailable")
	}
	op, err := store.WorkbenchTaskOperation(requestID)
	if err != nil || op.Phase != common.WorkbenchOperationNeedsReview || op.Binding == nil || op.Binding.IssueID == 0 {
		return op.Receipt, err
	}
	connection, client, releaseCredential, err := s.githubOperationClient(ctx, op)
	if err != nil {
		return op.Receipt, err
	}
	defer releaseCredential()
	task, err := client.Issue(ctx, connection.WorkbenchConnection, op.Binding.IssueNumber)
	if err != nil {
		if invalidateErr := s.invalidateGitHubOperationAuthorization(connection, err); invalidateErr != nil {
			return op.Receipt, invalidateErr
		}
		return op.Receipt, err
	}
	if task.NativeFields["issue_id"] != strconv.FormatInt(op.Binding.IssueID, 10) {
		return op.Receipt, nil
	}
	if op.Input.State != "" && task.State != op.Input.State {
		return op.Receipt, nil
	}
	workbench, err := s.workbenchStore()
	if err != nil {
		return op.Receipt, err
	}
	before, err := workbench.WorkbenchObjects(connection.ID)
	if err != nil {
		return op.Receipt, err
	}
	after := mergeGitHubIncremental(before, []common.WorkbenchTask{task})
	matchWorkbench(connection.WorkbenchConnection, before, after)
	var taskID string
	for _, item := range after {
		if item.NativeFields["issue_id"] == task.NativeFields["issue_id"] {
			if op.Input.State != "" && item.State != op.Input.State {
				return op.Receipt, nil
			}
			taskID = item.ID
			break
		}
	}
	if taskID == "" {
		return op.Receipt, nil
	}
	receipt := op.Receipt
	receipt.SourceObjectID, receipt.SyncState, receipt.Reason = taskID, common.WorkbenchOperationConfirmed, ""
	if err := store.CommitWorkbenchTaskOperationPartial(connection.WorkbenchConnection, nil, before, after, time.Now().UTC(), connection.SnapshotVersion, receipt); err != nil {
		return op.Receipt, err
	}
	return receipt, nil
}
