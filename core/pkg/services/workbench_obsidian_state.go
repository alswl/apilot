package services

import (
	"context"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func (s *Service) setWorkbenchObsidianState(store workbenchStore, writer workbenchTaskWriteStore, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, fingerprint string, task common.WorkbenchTask) (common.WorkbenchTaskReceipt, error) {
	var empty common.WorkbenchTaskReceipt
	if (input.State != "done" && input.State != "open") || input.Title != "" || input.NotePath != "" || len(input.HeadingPath) != 0 {
		return empty, common.Err("INVALID_ARGUMENT", "Provide an explicit open or done state")
	}
	if s.workbenchActive[connection.ID] != nil {
		return empty, common.Err("CONFLICT", "Enable Obsidian writes and wait for the active synchronization to finish")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	path, content, replacement, conflict, err := prepareWorkbenchObsidianState(ctx, connection, input, task)
	if err != nil || conflict.SyncState != "" {
		return conflict, err
	}
	return s.writeWorkbenchObsidianNote(store, writer, connection, input, fingerprint, path, content, replacement)
}

// prepareWorkbenchObsidianState verifies the identity and computes a patch
// without modifying the source or accessing mutable service state.
func prepareWorkbenchObsidianState(ctx context.Context, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, task common.WorkbenchTask) (string, []byte, []byte, common.WorkbenchTaskReceipt, error) {
	return prepareWorkbenchObsidianStateWithCoverage(ctx, connection, input, task, false)
}

func prepareWorkbenchObsidianStateWithCoverage(ctx context.Context, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, task common.WorkbenchTask, nativeCovered bool) (string, []byte, []byte, common.WorkbenchTaskReceipt, error) {
	var empty common.WorkbenchTaskReceipt
	if (input.State != "done" && input.State != "open") || input.Title != "" || input.NotePath != "" || len(input.HeadingPath) != 0 {
		return "", nil, nil, empty, common.Err("INVALID_ARGUMENT", "Provide an explicit open or done state")
	}
	conflict := common.WorkbenchTaskReceipt{
		RequestID: input.RequestID, ConnectionID: connection.ID, SourceObjectID: task.ID, SyncState: "conflict",
	}
	if !workbenchTaskMatchesTarget(task, input.TargetVersion) {
		conflict.Reason = "The saved task was refreshed. Review the latest task and try again."
		return "", nil, nil, conflict, nil
	}
	path := task.NativeFields["path"]
	var tasks []common.WorkbenchTask
	var err error
	if task.NativeID == "" || nativeCovered {
		// Location-bound identities or continuous cross-note coverage allow
		// this observation to verify the affected note independently.
		tasks, err = integrations.ReadWorkbenchObsidianTasksInNote(ctx, connection.WorkbenchConnection, path)
	} else {
		tasks, err = readWorkbenchObsidianWrittenVault(ctx, connection.WorkbenchConnection)
	}
	if err != nil {
		return "", nil, nil, empty, common.Err("CONFLICT", "Could not verify task identity in the vault")
	}
	matches := countWorkbenchObsidianIdentity(task, tasks)
	if matches == 0 {
		conflict.Reason = "The task no longer matches the note. Synchronize the vault and try again."
		return "", nil, nil, conflict, nil
	}
	if matches > 1 {
		conflict.Reason = "More than one Obsidian task matches the saved task"
		return "", nil, nil, conflict, nil
	}
	content, err := integrations.ReadWorkbenchObsidianNote(connection.WorkbenchConnection, path)
	if err != nil {
		return "", nil, nil, empty, common.Err("CONFLICT", "Could not read the task note")
	}
	replacement, err := integrations.SetWorkbenchObsidianCheckbox(connection.WorkbenchConnection, content, task, input.State, time.Now())
	if err != nil {
		if problem, ok := err.(*common.Error); ok && problem.Code == "STALE_REVISION" {
			conflict.Reason = problem.Message
			return "", nil, nil, conflict, nil
		}
		return "", nil, nil, empty, err
	}
	return path, content, replacement, empty, nil
}

// countWorkbenchObsidianIdentity treats duplicated source IDs as ambiguous,
// even when only one of the duplicates has the requested state.
func countWorkbenchObsidianIdentity(original common.WorkbenchTask, candidates []common.WorkbenchTask) int {
	matches := 0
	for _, candidate := range candidates {
		if original.NativeID != "" && candidate.NativeID == original.NativeID ||
			original.NativeID == "" && integrations.MatchesWorkbenchObsidianTask(original, candidate) {
			matches++
		}
	}
	return matches
}

func countWorkbenchLocationIdentity(original common.WorkbenchTask, candidates []common.WorkbenchTask) int {
	count := 0
	original.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(original.Summary)
	for _, candidate := range candidates {
		candidate.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(candidate.Summary)
		if integrations.MatchesWorkbenchObsidianTask(original, candidate) {
			count++
		}
	}
	return count
}
