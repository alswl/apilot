package services

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchNoteGate struct {
	token chan struct{}
	users int
}

type workbenchNoteGates struct {
	mu    sync.Mutex
	items map[string]*workbenchNoteGate
}

// lock counts both the holder and waiters so an idle note gate can be removed
// without allowing another caller to bypass a waiter.
func (g *workbenchNoteGates) lock(ctx context.Context, key string) (func(), error) {
	g.mu.Lock()
	if g.items == nil {
		g.items = make(map[string]*workbenchNoteGate)
	}
	gate := g.items[key]
	if gate == nil {
		gate = &workbenchNoteGate{token: make(chan struct{}, 1)}
		g.items[key] = gate
	}
	gate.users++
	g.mu.Unlock()
	releaseUser := func() {
		g.mu.Lock()
		gate.users--
		if gate.users == 0 {
			delete(g.items, key)
		}
		g.mu.Unlock()
	}
	select {
	case gate.token <- struct{}{}:
		return func() {
			<-gate.token
			releaseUser()
		}, nil
	case <-ctx.Done():
		releaseUser()
		return nil, ctx.Err()
	}
}

// executeObsidianTaskOperation prepares the conditional write before claiming
// dispatch. Once claimed, all uncertain outcomes require source reconciliation.
func (s *Service) executeObsidianTaskOperation(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, error) {
	ops, ok := s.store.(workbenchTaskOperationStore)
	if !ok {
		return common.WorkbenchTaskReceipt{}, common.Err("INTERNAL", "Task operation storage unavailable")
	}
	op, err := ops.WorkbenchTaskOperation(requestID)
	if err != nil {
		return common.WorkbenchTaskReceipt{}, err
	}
	if op.Phase != common.WorkbenchOperationAccepted {
		return op.Receipt, nil
	}
	started := time.Now()
	var gateDuration, prepareDuration, writeDuration, reconcileDuration time.Duration
	var prepareStarted time.Time
	result := "not_dispatched"
	defer func() {
		if gateDuration == 0 {
			gateDuration = time.Since(started)
		}
		if prepareDuration == 0 && !prepareStarted.IsZero() {
			prepareDuration = time.Since(prepareStarted)
		}
		log.Printf("workbench operation request=%q connection=%q result=%s total=%s wait=%s prepare=%s write=%s reconcile=%s",
			requestID, op.Receipt.ConnectionID, result, time.Since(started).Round(time.Millisecond), gateDuration.Round(time.Millisecond),
			prepareDuration.Round(time.Millisecond), writeDuration.Round(time.Millisecond), reconcileDuration.Round(time.Millisecond))
	}()
	connection, err := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
	if err != nil {
		return op.Receipt, err
	}
	if connection.Kind != common.WorkbenchObsidianTasks {
		return op.Receipt, common.Err("INVALID_ARGUMENT", "Operation requires an Obsidian connection")
	}
	notePath := op.Input.NotePath
	if op.Input.SourceObjectID != "" {
		store, err := s.workbenchStore()
		if err != nil {
			return op.Receipt, err
		}
		task, err := store.WorkbenchObject(op.Input.SourceObjectID)
		if err != nil {
			if problem, ok := errors.AsType[*common.Error](err); ok && problem.Code == "NOT_FOUND" {
				receipt := op.Receipt
				receipt.SyncState, receipt.Reason = common.WorkbenchOperationConflict, "Task is no longer visible"
				if updateErr := ops.AdvanceWorkbenchTaskOperation(requestID, common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, receipt); updateErr != nil {
					return op.Receipt, updateErr
				}
				return receipt, nil
			}
			return op.Receipt, err
		}
		notePath = task.NativeFields["path"]
	}
	if !filepath.IsLocal(notePath) {
		return op.Receipt, common.Err("INVALID_ARGUMENT", "Invalid source note path")
	}
	vault, err := filepath.EvalSymlinks(connection.Vault)
	if err != nil {
		return op.Receipt, err
	}
	release, err := s.operationNotes.lock(ctx, filepath.Join(vault, notePath))
	if err != nil {
		return op.Receipt, err
	}
	defer release()
	gateDuration = time.Since(started)
	prepareStarted = time.Now()
	// A duplicate request may have completed while waiting for the note.
	op, err = ops.WorkbenchTaskOperation(requestID)
	if err != nil || op.Phase != common.WorkbenchOperationAccepted {
		return op.Receipt, err
	}
	finish := func(from, state, reason string) (common.WorkbenchTaskReceipt, error) {
		receipt := op.Receipt
		receipt.SyncState, receipt.Reason = state, reason
		if err := ops.AdvanceWorkbenchTaskOperation(requestID, from, state, receipt); err != nil {
			return op.Receipt, err
		}
		return receipt, nil
	}
	prepareFailure := func(err error) (common.WorkbenchTaskReceipt, error) {
		problem, ok := errors.AsType[*common.Error](err)
		if ok && (problem.Code == "STALE_REVISION" || problem.Code == "INVALID_ARGUMENT") {
			return finish(common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, problem.Message)
		}
		return op.Receipt, err
	}
	var path string
	var content, replacement []byte
	var nativeCovered bool
	var preparedTask common.WorkbenchTask
	if op.Input.SourceObjectID != "" {
		store, err := s.workbenchStore()
		if err != nil {
			return op.Receipt, err
		}
		task, err := store.WorkbenchObject(op.Input.SourceObjectID)
		if err != nil {
			problem, ok := errors.AsType[*common.Error](err)
			if ok && problem.Code == "NOT_FOUND" {
				return finish(common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, "Task is no longer visible")
			}
			return op.Receipt, err
		}
		if task.NativeFields["path"] != notePath {
			return op.Receipt, common.Err("STALE_REVISION", "Task note changed while waiting for dispatch")
		}
		preparedTask = task
		known, err := store.WorkbenchObjects(connection.ID)
		if err != nil {
			return op.Receipt, err
		}
		if task.NativeID == "" {
			if countWorkbenchLocationIdentity(task, known) != 1 {
				return finish(common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, "More than one saved task matches this location")
			}
		} else if _, ok := ops.(interface {
			ClaimWorkbenchTaskOperationIfSnapshot(string, int64) (common.WorkbenchTaskOperation, error)
		}); ok && !connection.BridgeConfigured {
			s.mu.Lock()
			current, currentErr := s.WorkbenchSnapshot(connection.ID)
			nativeCovered = currentErr == nil && current.Version == connection.Version && current.SnapshotVersion == connection.SnapshotVersion &&
				s.workbenchIdentityCoverageLocked(connection.WorkbenchConnection, notePath) && countWorkbenchObsidianIdentity(task, known) == 1
			s.mu.Unlock()
		}
		if task.State == op.Input.State && !workbenchTaskMatchesTarget(task, op.Input.TargetVersion) {
			receipt, err := finish(common.WorkbenchOperationAccepted, common.WorkbenchOperationNeedsReview, "")
			if err != nil {
				return receipt, err
			}
			result = common.WorkbenchOperationNeedsReview
			reconcileStarted := time.Now()
			receipt, err = s.reconcileWorkbenchOperationWithRetry(ctx, requestID)
			reconcileDuration = time.Since(reconcileStarted)
			if err == nil {
				result = receipt.SyncState
			}
			return receipt, err
		}
		var conflict common.WorkbenchTaskReceipt
		path, content, replacement, conflict, err = prepareWorkbenchObsidianStateWithCoverage(ctx, connection, op.Input, task, nativeCovered)
		if err != nil {
			return prepareFailure(err)
		}
		if conflict.SyncState != "" {
			return finish(common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, conflict.Reason)
		}
	} else {
		path = op.Input.NotePath
		content, replacement, err = prepareWorkbenchObsidianCreation(connection.WorkbenchConnection, op.Input, op.Fingerprint)
		if err != nil {
			return prepareFailure(err)
		}
		marker := obsidianCreationMarker(op.Fingerprint)
		if _, ok := ops.(interface {
			ClaimWorkbenchTaskOperationIfSnapshot(string, int64) (common.WorkbenchTaskOperation, error)
		}); ok && !connection.BridgeConfigured {
			store, err := s.workbenchStore()
			if err != nil {
				return op.Receipt, err
			}
			known, err := store.WorkbenchObjects(connection.ID)
			if err != nil {
				return op.Receipt, err
			}
			unused := true
			for _, task := range known {
				if task.NativeID == marker {
					unused = false
				}
			}
			s.mu.Lock()
			current, currentErr := s.WorkbenchSnapshot(connection.ID)
			nativeCovered = unused && currentErr == nil && current.Version == connection.Version && current.SnapshotVersion == connection.SnapshotVersion &&
				s.workbenchIdentityCoverageLocked(connection.WorkbenchConnection, path)
			s.mu.Unlock()
		}
		if !nativeCovered {
			// A marker may have been copied into another note. Without a
			// continuous coverage proof, check the whole source before writing.
			observed, err := readWorkbenchObsidianWrittenVault(ctx, connection.WorkbenchConnection)
			if err != nil {
				return op.Receipt, common.Err("CONFLICT", "Could not verify the new task identity in the vault")
			}
			for _, task := range observed {
				if task.NativeID == marker {
					return finish(common.WorkbenchOperationAccepted, common.WorkbenchOperationConflict, "The task creation marker already exists in the vault")
				}
			}
		}
	}
	write, err := s.prepareWorkbenchObsidianNoteWriteUnlocked(ctx, connection, path, content, replacement)
	if err != nil {
		return prepareFailure(err)
	}
	// Serialize the dispatch boundary with StopAdmission and configuration changes.
	s.mu.Lock()
	if s.stopping || ctx.Err() != nil {
		s.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return op.Receipt, err
		}
		return op.Receipt, common.Err("CONFLICT", "App is stopping")
	}
	var claimed common.WorkbenchTaskOperation
	if nativeCovered && !s.workbenchIdentityCoverageLocked(connection.WorkbenchConnection, path) {
		s.mu.Unlock()
		return op.Receipt, common.Err("STALE_REVISION", "Source identity coverage changed before dispatch")
	}
	if fenced, ok := ops.(interface {
		ClaimWorkbenchTaskOperationIfSnapshot(string, int64) (common.WorkbenchTaskOperation, error)
	}); ok {
		expected := connection.SnapshotVersion
		if connection.BridgeConfigured {
			store, storeErr := s.workbenchStore()
			if storeErr != nil {
				s.mu.Unlock()
				return op.Receipt, storeErr
			}
			current, snapshotErr := s.WorkbenchSnapshot(connection.ID)
			known, knownErr := store.WorkbenchObjects(connection.ID)
			if snapshotErr != nil || knownErr != nil {
				s.mu.Unlock()
				if snapshotErr != nil {
					return op.Receipt, snapshotErr
				}
				return op.Receipt, knownErr
			}
			valid := current.Version == connection.Version
			if op.Input.SourceObjectID != "" {
				matches := 0
				for _, task := range known {
					if task.ID == preparedTask.ID && workbenchFingerprint(task) == workbenchFingerprint(preparedTask) {
						matches++
					}
				}
				valid = valid && matches == 1 && countWorkbenchObsidianIdentity(preparedTask, known) == 1
			} else {
				for _, task := range known {
					if task.NativeID == obsidianCreationMarker(op.Fingerprint) {
						valid = false
					}
				}
			}
			if !valid {
				s.mu.Unlock()
				return op.Receipt, common.Err("STALE_REVISION", "Source mapping changed before dispatch")
			}
			expected = current.SnapshotVersion
		}
		claimed, err = fenced.ClaimWorkbenchTaskOperationIfSnapshot(requestID, expected)
	} else {
		claimed, err = ops.ClaimWorkbenchTaskOperation(requestID)
	}
	s.mu.Unlock()
	if err != nil {
		return op.Receipt, err
	}
	op = claimed
	prepareDuration = time.Since(prepareStarted)
	result = common.WorkbenchOperationDispatching
	writeStarted := time.Now()
	err = write()
	writeDuration = time.Since(writeStarted)
	if err != nil {
		problem, ok := errors.AsType[*common.Error](err)
		if ok && problem.Code == "STALE_REVISION" {
			result = common.WorkbenchOperationConflict
			return finish(common.WorkbenchOperationDispatching, common.WorkbenchOperationConflict, problem.Message)
		}
	}
	receipt, err := finish(common.WorkbenchOperationDispatching, common.WorkbenchOperationNeedsReview, "")
	if err != nil {
		return receipt, err
	}
	verifyCtx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	result = common.WorkbenchOperationNeedsReview
	reconcileStarted := time.Now()
	receipt, err = s.reconcileWorkbenchOperationWithRetry(verifyCtx, requestID)
	reconcileDuration = time.Since(reconcileStarted)
	if err == nil {
		result = receipt.SyncState
	}
	return receipt, err
}

func (s *Service) reconcileWorkbenchOperationWithRetry(ctx context.Context, requestID string) (common.WorkbenchTaskReceipt, error) {
	var receipt common.WorkbenchTaskReceipt
	var err error
	for attempt := 0; attempt <= s.obsidianSync.MaxRetries; attempt++ {
		receipt, err = s.reconcileWorkbenchTaskOperation(ctx, requestID)
		problem, stale := errors.AsType[*common.Error](err)
		if !stale || problem.Code != "STALE_REVISION" || attempt == s.obsidianSync.MaxRetries {
			return receipt, err
		}
		select {
		case <-ctx.Done():
			return receipt, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return receipt, err
}

// scheduleWorkbenchOperation starts at most one worker per request and joins
// it before the store is closed. Note gates serialize different request IDs.
func (s *Service) scheduleWorkbenchOperation(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.operationRunning[requestID] {
		return
	}
	s.operationRunning[requestID] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { s.mu.Lock(); delete(s.operationRunning, requestID); s.mu.Unlock() }()
		store, ok := s.store.(workbenchTaskOperationStore)
		if !ok {
			return
		}
		for attempt := 0; attempt <= s.obsidianSync.MaxRetries; attempt++ {
			ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
			op, lookupErr := store.WorkbenchTaskOperation(requestID)
			if lookupErr != nil {
				cancel()
				return
			}
			connection, lookupErr := s.WorkbenchSnapshot(op.Receipt.ConnectionID)
			if lookupErr != nil {
				cancel()
				return
			}
			var err error
			if connection.Kind == common.WorkbenchGitHubRepository {
				_, err = s.executeGitHubTaskOperation(ctx, requestID)
			} else {
				_, err = s.executeObsidianTaskOperation(ctx, requestID)
			}
			cancel()
			if err == nil || s.ctx.Err() != nil {
				return
			}
			op, lookupErr = store.WorkbenchTaskOperation(requestID)
			if lookupErr != nil || op.Phase != common.WorkbenchOperationAccepted {
				return
			}
			if attempt == s.obsidianSync.MaxRetries {
				op.Receipt.SyncState = common.WorkbenchOperationNotExecuted
				op.Receipt.Reason = "The operation could not be dispatched: " + err.Error()
				_ = store.AdvanceWorkbenchTaskOperation(requestID, common.WorkbenchOperationAccepted, common.WorkbenchOperationNotExecuted, op.Receipt)
				return
			}
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
}

// scheduleWorkbenchReconciliation only observes an already dispatched request;
// it never repeats the source write.
func (s *Service) scheduleWorkbenchReconciliation(requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping || s.operationRunning[requestID] {
		return
	}
	s.operationRunning[requestID] = true
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() { s.mu.Lock(); delete(s.operationRunning, requestID); s.mu.Unlock() }()
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		_, _ = s.reconcileWorkbenchOperationWithRetry(ctx, requestID)
	}()
}
