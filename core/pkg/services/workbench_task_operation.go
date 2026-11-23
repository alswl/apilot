package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

// SubmitWorkbenchTaskOperation persists an Obsidian request before scheduling it.
// Local operations settle in their existing database transaction.
func (s *Service) SubmitWorkbenchTaskOperation(input common.WorkbenchTaskWrite) (common.WorkbenchTaskReceipt, error) {
	if strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 256 || input.ConnectionVersion < 1 {
		return common.WorkbenchTaskReceipt{}, common.Err("INVALID_ARGUMENT", "Request ID and connection version are required")
	}
	operations, ok := s.store.(workbenchTaskOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "Task operation storage unavailable")
	}
	existing, lookupErr := operations.WorkbenchTaskOperation(input.RequestID)
	if lookupErr == nil {
		if existing.Fingerprint != workbenchFingerprint(input) {
			return common.WorkbenchTaskReceipt{}, common.Err("CONFLICT", "Request ID was already used with different input")
		}
		connection, err := s.WorkbenchSnapshot(existing.Receipt.ConnectionID)
		if err != nil {
			return common.WorkbenchTaskReceipt{}, err
		}
		if err := s.workbenchOperationVisible(connection, existing); err != nil {
			return common.WorkbenchTaskReceipt{}, err
		}
		if existing.Phase == "legacy" && existing.Receipt.SyncState == common.WorkbenchOperationNeedsReview && connection.Kind == common.WorkbenchObsidianTasks {
			restorer, ok := s.store.(interface {
				RestoreLegacyWorkbenchTaskOperation(common.WorkbenchTaskWrite, string) (common.WorkbenchTaskOperation, error)
			})
			if !ok {
				return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "Legacy task operation recovery unavailable")
			}
			s.mu.Lock()
			if s.stopping {
				s.mu.Unlock()
				return common.WorkbenchTaskReceipt{}, common.Err("CONFLICT", "App is stopping")
			}
			existing, err = restorer.RestoreLegacyWorkbenchTaskOperation(input, workbenchFingerprint(input))
			s.mu.Unlock()
			if err != nil {
				return common.WorkbenchTaskReceipt{}, err
			}
		}
		if existing.Phase == common.WorkbenchOperationAccepted && (connection.ParentConnectionID != "" || connection.Kind == common.WorkbenchObsidianTasks || connection.Kind == common.WorkbenchGitHubRepository) {
			s.scheduleWorkbenchOperation(input.RequestID)
		} else if existing.Phase == common.WorkbenchOperationNeedsReview && (connection.Kind == common.WorkbenchObsidianTasks || connection.Kind == common.WorkbenchGitHubRepository) {
			if connection.Kind == common.WorkbenchGitHubRepository && (existing.Binding == nil || existing.Binding.IssueID == 0) {
				return existing.Receipt, nil
			}
			s.scheduleWorkbenchReconciliation(input.RequestID)
			return s.WorkbenchTaskOperationReceipt(input.RequestID)
		}
		return existing.Receipt, nil
	}
	problem, notFound := errors.AsType[*common.Error](lookupErr)
	if !notFound || problem.Code != "NOT_FOUND" {
		return common.WorkbenchTaskReceipt{}, lookupErr
	}
	connectionID := input.ConnectionID
	if input.SourceObjectID != "" {
		store, err := s.workbenchStore()
		if err != nil {
			return common.WorkbenchTaskReceipt{}, err
		}
		task, err := store.WorkbenchObject(input.SourceObjectID)
		if err != nil {
			return common.WorkbenchTaskReceipt{}, err
		}
		if connectionID != "" && connectionID != task.ConnectionID {
			return common.WorkbenchTaskReceipt{}, common.Err("INVALID_ARGUMENT", "Task belongs to another connection")
		}
		connectionID = task.ConnectionID
	}
	connection, err := s.WorkbenchSnapshot(connectionID)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	if connection.ParentConnectionID != "" {
		return s.submitConnectorOperation(input, connection)
	}
	if connection.Kind == common.WorkbenchLocal {
		return s.writeWorkbenchTaskLegacy(input)
	}
	if len(input.Fields) > 0 || input.Action != "" || len(input.ActionFields) > 0 {
		return common.WorkbenchTaskReceipt{}, common.Err("INVALID_ARGUMENT", "This legacy source does not support typed operations")
	}
	if connection.Kind != common.WorkbenchObsidianTasks && connection.Kind != common.WorkbenchGitHubRepository {
		return common.WorkbenchTaskReceipt{}, common.Err("CONFLICT", "Connector does not support task writes")
	}
	op, _, err := s.admitWorkbenchTaskOperation(input)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	if op.Phase == common.WorkbenchOperationAccepted {
		s.scheduleWorkbenchOperation(op.Receipt.RequestID)
	}
	return op.Receipt, nil
}

// WriteWorkbenchTask waits for the shared operation for compatibility callers.
func (s *Service) WriteWorkbenchTask(input common.WorkbenchTaskWrite) (common.WorkbenchTaskReceipt, error) {
	receipt, err := s.SubmitWorkbenchTaskOperation(input)
	if err != nil || receipt.SyncState != common.WorkbenchOperationAccepted && receipt.SyncState != common.WorkbenchOperationDispatching && receipt.SyncState != common.WorkbenchOperationNeedsReview {
		return receipt, err
	}
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return receipt, nil
		case <-timer.C:
			return receipt, nil
		case <-ticker.C:
			receipt, err = s.WorkbenchTaskOperationReceipt(input.RequestID)
			if err != nil {
				return receipt, err
			}
			if receipt.SyncState == common.WorkbenchOperationNeedsReview {
				s.mu.Lock()
				running := s.operationRunning[input.RequestID]
				s.mu.Unlock()
				if running {
					continue
				}
				ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
				defer cancel()
				return s.ReconcileWorkbenchTaskOperation(ctx, input.RequestID)
			}
			if receipt.SyncState != common.WorkbenchOperationAccepted && receipt.SyncState != common.WorkbenchOperationDispatching {
				return receipt, nil
			}
		}
	}
}

// WorkbenchTaskOperationReceipt returns a receipt for a currently visible connection.
func (s *Service) WorkbenchTaskOperationReceipt(requestID string) (common.WorkbenchTaskReceipt, error) {
	store, ok := s.store.(workbenchTaskOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "Task operation storage unavailable")
	}
	op, err := store.WorkbenchTaskOperation(requestID)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	connection, err := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	if err := s.workbenchOperationVisible(connection, op); err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	s.mu.Lock()
	running := s.operationRunning[requestID]
	s.mu.Unlock()
	if running && op.Receipt.SyncState == common.WorkbenchOperationNeedsReview {
		op.Receipt.SyncState = common.WorkbenchOperationDispatching
	}
	return op.Receipt, nil
}

func (s *Service) workbenchOperationVisible(connection common.WorkbenchSnapshot, op common.WorkbenchTaskOperation) error {
	if connection.ParentConnectionID != "" {
		scope, _, err := s.readableConnectorScope(connection.ID)
		if err != nil || op.Binding == nil || op.Binding.Connector == nil || scope.Context.AccountRef != op.Binding.Connector.Mutation.AccountRef {
			return common.Err("NOT_FOUND", "Unknown task operation")
		}
		if op.Receipt.SourceObjectID != "" {
			if _, err := s.WorkbenchTask(op.Receipt.SourceObjectID); err != nil {
				return err
			}
		}
		return nil
	}
	if connection.Kind != common.WorkbenchGitHubRepository {
		return nil
	}
	store, ok := s.store.(workbenchGitHubStore)
	if !ok {
		return common.Err("NOT_FOUND", "Unknown task operation")
	}
	progress, err := store.WorkbenchGitHubProgress(connection.ID)
	if err != nil || progress.AccessState != "available" || op.Binding == nil || op.Binding.AccountID != progress.AccountID || op.Binding.RepositoryID != progress.RepositoryID {
		return common.Err("NOT_FOUND", "Unknown task operation")
	}
	return nil
}

// ReconcileWorkbenchTaskOperation checks the stored request without resending it.
func (s *Service) ReconcileWorkbenchTaskOperation(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, error) {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return common.WorkbenchTaskReceipt{}, common.Err("CONFLICT", "App is stopping")
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if _, err := s.WorkbenchTaskOperationReceipt(requestID); err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	return s.reconcileWorkbenchOperationWithRetry(ctx, requestID)
}

type workbenchTaskOperationStore interface {
	AdmitWorkbenchTaskOperation(common.WorkbenchTaskWrite, string, string) (common.WorkbenchTaskOperation, bool, error)
	WorkbenchTaskOperation(string) (common.WorkbenchTaskOperation, error)
	PendingWorkbenchTaskOperations() ([]common.WorkbenchTaskOperation, error)
	ClaimWorkbenchTaskOperation(string) (common.WorkbenchTaskOperation, error)
	AdvanceWorkbenchTaskOperation(string, string, string, common.WorkbenchTaskReceipt) error
}

// admitWorkbenchTaskOperation performs only local validation and a short storage
// transaction. Source verification and I/O belong to the background executor.
func (s *Service) admitWorkbenchTaskOperation(input common.WorkbenchTaskWrite) (common.WorkbenchTaskOperation, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty common.WorkbenchTaskOperation
	if s.stopping {
		return empty, false, common.Err("CONFLICT", "App is stopping")
	}
	if strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 256 || input.ConnectionVersion < 1 {
		return empty, false, common.Err("INVALID_ARGUMENT", "Request ID and connection version are required")
	}
	operations, ok := s.store.(workbenchTaskOperationStore)
	if !ok {
		return empty, false, common.Err("INTERNAL", "Task operation storage unavailable")
	}
	store, err := s.workbenchStore()
	if err != nil {
		return empty, false, err
	}
	fingerprint := workbenchFingerprint(input)
	existing, err := operations.WorkbenchTaskOperation(input.RequestID)
	if err == nil {
		if existing.Fingerprint != fingerprint {
			return empty, false, common.Err("CONFLICT", "Request ID was already used with different input")
		}
		if _, err := s.WorkbenchSnapshot(existing.Receipt.ConnectionID); err != nil {
			return empty, false, err
		}
		return existing, false, nil
	}
	problem, ok := errors.AsType[*common.Error](err)
	if !ok || problem.Code != "NOT_FOUND" {
		return empty, false, err
	}
	connectionID := input.ConnectionID
	if input.SourceObjectID != "" {
		if (input.State != "open" && input.State != "done") || input.Title != "" || input.Body != "" || input.NotePath != "" || len(input.HeadingPath) != 0 {
			return empty, false, common.Err("INVALID_ARGUMENT", "Provide an explicit open or done state")
		}
		task, err := store.WorkbenchObject(input.SourceObjectID)
		if err != nil {
			return empty, false, err
		}
		if connectionID != "" && connectionID != task.ConnectionID {
			return empty, false, common.Err("INVALID_ARGUMENT", "Task belongs to another connection")
		}
		connectionID = task.ConnectionID
	} else if strings.TrimSpace(input.Title) == "" || len(strings.TrimSpace(input.Title)) > 1024 || strings.ContainsAny(input.Title, "\r\n\x00") || input.State != "" {
		return empty, false, common.Err("INVALID_ARGUMENT", "Provide a valid single-line task title")
	}
	connection, err := s.WorkbenchSnapshot(connectionID)
	if err != nil {
		return empty, false, err
	}
	if err := s.workbenchPluginAvailable(connection.Kind); err != nil {
		return empty, false, err
	}
	if connection.Paused || connection.Version != input.ConnectionVersion {
		return empty, false, common.Err("CONFLICT", "Connection is not writable with this configuration")
	}
	if (input.SourceObjectID == "" && !connection.Capabilities.Create) || (input.SourceObjectID != "" && !connection.Capabilities.SetState) {
		return empty, false, common.Err("CONFLICT", "Connector does not support this task write")
	}
	if connection.Kind != common.WorkbenchGitHubRepository && input.Body != "" {
		return empty, false, common.Err("INVALID_ARGUMENT", "Body is supported for GitHub issue creation")
	}
	if connection.Kind == common.WorkbenchLocal && input.SourceObjectID == "" && (input.NotePath != "" || len(input.HeadingPath) != 0 || input.TargetVersion != "") {
		return empty, false, common.Err("INVALID_ARGUMENT", "Provide a Local task without a source location")
	}
	return operations.AdmitWorkbenchTaskOperation(input, connectionID, fingerprint)
}
