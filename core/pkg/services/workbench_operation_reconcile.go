package services

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type workbenchOperationCommitter interface {
	CommitWorkbenchTaskOperation(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, time.Time, int64, common.WorkbenchTaskReceipt) error
}

func (s *Service) recoverWorkbenchOperations(store workbenchTaskOperationStore) {
	defer s.wg.Done()
	operations, err := store.PendingWorkbenchTaskOperations()
	if err != nil {
		return
	}
	// Resume undispatched requests first. Reconciliation can require a slow
	// source read, so an older uncertain request must not delay unrelated work.
	for _, operation := range operations {
		if s.ctx.Err() != nil {
			return
		}
		if operation.Phase == common.WorkbenchOperationAccepted {
			connection, err := s.WorkbenchSnapshot(operation.Receipt.ConnectionID)
			if err == nil && (connection.Kind == common.WorkbenchObsidianTasks || connection.Kind == common.WorkbenchGitHubRepository) {
				s.scheduleWorkbenchOperation(operation.Receipt.RequestID)
			}
		}
	}
	for _, operation := range operations {
		if s.ctx.Err() != nil {
			return
		}
		if operation.Phase != common.WorkbenchOperationNeedsReview {
			continue
		}
		if operation.Binding != nil && operation.Binding.RepositoryID != 0 && operation.Binding.IssueID == 0 {
			continue
		}
		s.mu.Lock()
		if s.stopping || s.operationRunning[operation.Receipt.RequestID] {
			s.mu.Unlock()
			continue
		}
		s.operationRunning[operation.Receipt.RequestID] = true
		s.mu.Unlock()
		for attempt := 0; attempt <= s.obsidianSync.MaxRetries; attempt++ {
			ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
			_, err := s.reconcileWorkbenchTaskOperation(ctx, operation.Receipt.RequestID)
			cancel()
			problem, stale := errors.AsType[*common.Error](err)
			if !stale || problem.Code != "STALE_REVISION" || attempt == s.obsidianSync.MaxRetries {
				break
			}
			select {
			case <-s.ctx.Done():
				s.mu.Lock()
				delete(s.operationRunning, operation.Receipt.RequestID)
				s.mu.Unlock()
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
		s.mu.Lock()
		delete(s.operationRunning, operation.Receipt.RequestID)
		s.mu.Unlock()
	}
}

// reconcileWorkbenchTaskOperation checks the original request against the source.
// An inconclusive observation retains the original needs_review receipt.
func (s *Service) reconcileWorkbenchTaskOperation(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, error) {
	if store, ok := s.store.(workbenchTaskOperationStore); ok {
		op, err := store.WorkbenchTaskOperation(requestID)
		if err != nil {
			return op.Receipt, err
		}
		connection, err := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
		if err != nil {
			return op.Receipt, err
		}
		if connection.Kind == common.WorkbenchGitHubRepository {
			return s.reconcileGitHubTaskOperation(ctx, requestID)
		}
	}

	if receipt, handled, err := s.reconcileWorkbenchObservedCreation(ctx, requestID); handled || err != nil {
		return receipt, err
	}
	if receipt, handled, err := s.reconcileWorkbenchObservedOperation(ctx, requestID); handled || err != nil {
		return receipt, err
	}
	return s.reconcileWorkbenchTaskOperationWithReader(ctx, requestID, readWorkbenchObsidianWrittenVault)
}

func (s *Service) reconcileWorkbenchObservedCreation(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, bool, error) {
	operations, ok := s.store.(workbenchTaskOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, false, nil
	}
	committer, ok := s.store.(interface {
		CommitWorkbenchObservedCreation(common.WorkbenchConnection, int64, common.WorkbenchTask, common.WorkbenchAttempt, common.WorkbenchTaskReceipt) error
	})
	if !ok {
		return common.WorkbenchTaskReceipt{}, false, nil
	}
	op, err := operations.WorkbenchTaskOperation(requestID)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, true, err
	}
	if op.Phase != common.WorkbenchOperationNeedsReview || op.Input.SourceObjectID != "" || len(op.Fingerprint) < 32 {
		return op.Receipt, false, nil
	}
	connection, err := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
	if err != nil {
		return op.Receipt, true, err
	}
	if !workbenchOperationSourceMatches(connection, op) {
		return op.Receipt, true, nil
	}
	path := op.Input.NotePath
	s.mu.Lock()
	covered := s.workbenchIdentityCoverageLocked(connection.WorkbenchConnection, path)
	s.mu.Unlock()
	if !covered {
		return op.Receipt, false, nil
	}
	store, err := s.workbenchStore()
	if err != nil {
		return op.Receipt, true, err
	}
	known, err := store.WorkbenchObjects(connection.ID)
	if err != nil {
		return op.Receipt, true, err
	}
	marker := obsidianCreationMarker(op.Fingerprint)
	var prior common.WorkbenchTask
	knownCount := 0
	for _, task := range known {
		if task.NativeID == marker {
			prior, knownCount = task, knownCount+1
		}
	}
	if knownCount > 1 || knownCount == 1 && prior.NativeFields["path"] != path {
		return op.Receipt, false, nil
	}
	readConnection := connection.WorkbenchConnection
	readConnection.ReadMethod = ""
	observation, err := integrations.ObserveWorkbenchObsidianNote(ctx, readConnection, path)
	if err != nil || observation.Coverage != common.WorkbenchCoverageRead {
		return op.Receipt, false, nil
	}
	heading, _ := json.Marshal(op.Input.HeadingPath)
	var target common.WorkbenchTask
	matches := 0
	for _, task := range observation.Tasks {
		if task.NativeID == marker {
			target, matches = task, matches+1
		}
	}
	if matches != 1 || target.NativeFields["heading_path"] != string(heading) || ctx.Err() != nil {
		return op.Receipt, false, nil
	}
	now := time.Now().UTC()
	target.ConnectionID, target.Kind, target.Mode = connection.ID, connection.Kind, connection.Mode
	if knownCount == 1 {
		target.ID, target.SourceKey, target.Activity = prior.ID, prior.SourceKey, prior.Activity
	} else {
		target.ID, target.SourceKey = common.ID(), target.SourceKey+"\x000"
		target.Activity = []common.WorkbenchTaskEvent{{Time: now, Text: "从 Obsidian 导入"}}
	}
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), Trigger: "write", State: "success", Started: now, Finished: &now,
		Scope: "notes", Paths: []string{path}, Coverage: "complete", ConnectionVersion: connection.Version, SnapshotVersion: connection.SnapshotVersion}
	if knownCount == 0 {
		attempt.Added = 1
	} else {
		countWorkbenchChanges(&attempt, []common.WorkbenchTask{prior}, []common.WorkbenchTask{target})
	}
	receipt := op.Receipt
	receipt.SyncState, receipt.SourceObjectID, receipt.Reason = common.WorkbenchOperationConfirmed, target.ID, ""
	s.mu.Lock()
	if !s.workbenchIdentityCoverageLocked(connection.WorkbenchConnection, path) {
		s.mu.Unlock()
		return op.Receipt, false, nil
	}
	err = committer.CommitWorkbenchObservedCreation(connection.WorkbenchConnection, connection.SnapshotVersion, target, attempt, receipt)
	s.mu.Unlock()
	if err != nil {
		return op.Receipt, true, err
	}
	return receipt, true, nil
}

func workbenchOperationSourceMatches(connection common.WorkbenchSnapshot, op common.WorkbenchTaskOperation) bool {
	if connection.Kind != common.WorkbenchObsidianTasks || connection.PluginDisabled {
		return false
	}
	return connection.Version == op.Input.ConnectionVersion ||
		op.Binding != nil && connection.Vault == op.Binding.Vault && connection.Scope == op.Binding.Scope
}

func workbenchLocationBaselineMatches(op common.WorkbenchTaskOperation, current common.WorkbenchTask) bool {
	if op.Binding == nil || op.Binding.Baseline == nil {
		return workbenchTaskMatchesTarget(current, op.Input.TargetVersion)
	}
	baseline := *op.Binding.Baseline
	return baseline.ID == current.ID && baseline.NativeID == "" &&
		workbenchTaskMatchesTarget(baseline, op.Input.TargetVersion) && countWorkbenchLocationIdentity(baseline, []common.WorkbenchTask{current}) == 1
}

// Native IDs require continuously watched cross-note coverage; location-bound
// identities can be verified independently inside their original note.
func (s *Service) reconcileWorkbenchObservedOperation(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, bool, error) {
	operations, ok := s.store.(workbenchTaskOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, false, nil
	}
	committer, ok := s.store.(interface {
		CommitWorkbenchObservedOperation(common.WorkbenchConnection, int64, common.WorkbenchTask, common.WorkbenchAttempt, common.WorkbenchTaskReceipt) error
	})
	if !ok {
		return common.WorkbenchTaskReceipt{}, false, nil
	}
	op, err := operations.WorkbenchTaskOperation(requestID)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, true, err
	}
	if op.Phase != common.WorkbenchOperationNeedsReview || op.Input.SourceObjectID == "" {
		return op.Receipt, false, nil
	}
	connection, err := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
	if err != nil {
		return op.Receipt, true, err
	}
	if !workbenchOperationSourceMatches(connection, op) {
		return op.Receipt, true, nil
	}
	store, err := s.workbenchStore()
	if err != nil {
		return op.Receipt, true, err
	}
	original, err := store.WorkbenchObject(op.Input.SourceObjectID)
	if err != nil || original.ConnectionID != connection.ID {
		return op.Receipt, false, nil
	}
	known, err := store.WorkbenchObjects(connection.ID)
	if err != nil {
		return op.Receipt, true, err
	}
	path := original.NativeFields["path"]
	if original.NativeID == "" {
		if !workbenchLocationBaselineMatches(op, original) || countWorkbenchLocationIdentity(original, known) != 1 {
			return op.Receipt, false, nil
		}
	} else {
		s.mu.Lock()
		covered := s.workbenchIdentityCoverageLocked(connection.WorkbenchConnection, path)
		s.mu.Unlock()
		if !covered || countWorkbenchObsidianIdentity(original, known) != 1 {
			return op.Receipt, false, nil
		}
	}
	readConnection := connection.WorkbenchConnection
	readConnection.ReadMethod = ""
	observation, err := integrations.ObserveWorkbenchObsidianNote(ctx, readConnection, path)
	if err != nil || observation.Coverage != common.WorkbenchCoverageRead {
		return op.Receipt, false, nil
	}
	var target common.WorkbenchTask
	matches := 0
	for _, candidate := range observation.Tasks {
		if original.NativeID != "" {
			if candidate.NativeID == original.NativeID {
				target, matches = candidate, matches+1
			}
			continue
		}
		prior, current := original, candidate
		prior.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(prior.Summary)
		current.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(current.Summary)
		if integrations.MatchesWorkbenchObsidianTask(prior, current) {
			target, matches = candidate, matches+1
		}
	}
	if matches != 1 || target.State != op.Input.State || (target.NativeFields["done"] != "") != (op.Input.State == "done") {
		return op.Receipt, false, nil
	}
	if err := ctx.Err(); err != nil {
		return op.Receipt, true, err
	}
	target.ID, target.ConnectionID, target.Kind, target.Mode = original.ID, original.ConnectionID, original.Kind, original.Mode
	target.SourceKey, target.Activity = original.SourceKey, original.Activity
	now := time.Now().UTC()
	recordWorkbenchStateChange(original, &target, now)
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), Trigger: "write", State: "success", Started: now, Finished: &now,
		Scope: "notes", Paths: []string{path}, Coverage: "complete", ConnectionVersion: connection.Version, SnapshotVersion: connection.SnapshotVersion}
	countWorkbenchChanges(&attempt, []common.WorkbenchTask{original}, []common.WorkbenchTask{target})
	receipt := op.Receipt
	receipt.SyncState, receipt.SourceObjectID, receipt.Reason = common.WorkbenchOperationConfirmed, target.ID, ""
	s.mu.Lock()
	if original.NativeID != "" && !s.workbenchIdentityCoverageLocked(connection.WorkbenchConnection, path) {
		s.mu.Unlock()
		return op.Receipt, false, nil
	}
	err = committer.CommitWorkbenchObservedOperation(connection.WorkbenchConnection, connection.SnapshotVersion, target, attempt, receipt)
	s.mu.Unlock()
	if err != nil {
		return op.Receipt, true, err
	}
	return receipt, true, nil
}

func (s *Service) reconcileWorkbenchTaskOperationWithReader(ctx context.Context, requestID string, read workbenchReader) (common.WorkbenchTaskReceipt, error) {
	operations, ok := s.store.(workbenchTaskOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "Task operation storage unavailable")
	}
	committer, ok := s.store.(workbenchOperationCommitter)
	if !ok {
		return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "Task operation commit unavailable")
	}
	op, err := operations.WorkbenchTaskOperation(requestID)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	if op.Phase == "legacy" && op.Receipt.SyncState == common.WorkbenchOperationNeedsReview {
		return op.Receipt, common.Err("CONFLICT", "This older operation requires its original request for reconciliation")
	}
	if op.Phase != common.WorkbenchOperationNeedsReview {
		return op.Receipt, nil
	}
	connection, err := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
	if err != nil {
		return op.Receipt, err
	}
	if !workbenchOperationSourceMatches(connection, op) {
		return op.Receipt, nil
	}
	store, err := s.workbenchStore()
	if err != nil {
		return op.Receipt, err
	}
	before, err := store.WorkbenchObjects(connection.ID)
	if err != nil {
		return op.Receipt, err
	}
	tasks, err := read(ctx, connection.WorkbenchConnection)
	if err != nil {
		return op.Receipt, nil
	}
	ambiguous := ambiguousWorkbenchNativeIdentities(before, tasks)
	if op.Input.SourceObjectID != "" {
		var original *common.WorkbenchTask
		for i := range before {
			if before[i].ID == op.Input.SourceObjectID {
				original = &before[i]
				break
			}
		}
		if original == nil {
			return op.Receipt, nil
		}
		if original.NativeID == "" && !workbenchLocationBaselineMatches(op, *original) {
			return op.Receipt, nil
		}
		if ambiguous[original.NativeID] {
			return op.Receipt, nil
		}
		identities := 0
		for _, candidate := range tasks {
			if original.NativeID != "" {
				if candidate.NativeID == original.NativeID {
					identities++
				}
				continue
			}
			prior := *original
			prior.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(prior.Summary)
			candidate.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(candidate.Summary)
			if integrations.MatchesWorkbenchObsidianTask(prior, candidate) {
				identities++
			}
		}
		if identities != 1 {
			return op.Receipt, nil
		}
	}
	matchBefore, tasks, preserved := partitionWorkbenchAmbiguous(before, tasks, ambiguous)
	matchWorkbench(connection.WorkbenchConnection, matchBefore, tasks)
	now := time.Now().UTC()
	recordObsidianImport(connection.WorkbenchConnection, matchBefore, tasks, now)
	index, matches := -1, 0
	for i := range tasks {
		task := tasks[i]
		if op.Input.SourceObjectID != "" {
			if task.ID == op.Input.SourceObjectID && task.State == op.Input.State && (task.NativeFields["done"] != "") == (op.Input.State == "done") {
				index, matches = i, matches+1
			}
		} else {
			heading, _ := json.Marshal(op.Input.HeadingPath)
			if task.NativeID == obsidianCreationMarker(op.Fingerprint) {
				matches++
				if task.NativeFields["path"] == op.Input.NotePath && task.NativeFields["heading_path"] == string(heading) {
					index = i
				}
			}
		}
	}
	if matches != 1 || index < 0 {
		return op.Receipt, nil
	}
	if err := ctx.Err(); err != nil {
		return op.Receipt, err
	}
	targetID := tasks[index].ID
	seen := make(map[string]bool, len(tasks)+len(preserved))
	for _, task := range tasks {
		seen[task.ID] = true
	}
	for _, task := range preserved {
		seen[task.ID] = true
	}
	missing := false
	for _, task := range before {
		if !seen[task.ID] {
			preserved = append(preserved, task)
			missing = true
		}
	}
	tasks = append(tasks, preserved...)
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), Trigger: "write", State: "success", Started: now, Finished: &now,
		Scope: "full", Coverage: "complete", ConnectionVersion: connection.Version, SnapshotVersion: connection.SnapshotVersion}
	if len(ambiguous) > 0 || missing {
		attempt.State = "failed"
		attempt.Coverage = "partial"
		if len(ambiguous) > 0 {
			attempt.Error = "Source contains duplicate task IDs; previous values were retained"
		} else {
			attempt.Error = "Source removals require another verification"
		}
	}
	countWorkbenchChanges(&attempt, before, tasks)
	attempts := append([]common.WorkbenchAttempt{attempt}, connection.Attempts...)
	receipt := op.Receipt
	receipt.SyncState, receipt.SourceObjectID, receipt.Reason = common.WorkbenchOperationConfirmed, targetID, ""
	if len(ambiguous) > 0 || missing {
		partial, ok := s.store.(interface {
			CommitWorkbenchTaskOperationPartial(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, time.Time, int64, common.WorkbenchTaskReceipt) error
		})
		if !ok {
			return op.Receipt, nil
		}
		if err := partial.CommitWorkbenchTaskOperationPartial(connection.WorkbenchConnection, attempts[:min(20, len(attempts))], before, tasks, now, connection.SnapshotVersion, receipt); err != nil {
			return op.Receipt, err
		}
	} else if err := committer.CommitWorkbenchTaskOperation(connection.WorkbenchConnection, attempts[:min(20, len(attempts))], before, tasks, now, connection.SnapshotVersion, receipt); err != nil {
		return op.Receipt, err
	}
	if missing {
		s.enqueueWorkbenchSourceChange(store, connection.ID)
	}
	return receipt, nil
}
