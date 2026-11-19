package services

import (
	"context"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/integrations"
)

func (s *Service) setWorkbenchObsidianState(store workbenchStore, writer workbenchTaskWriteStore, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, fingerprint string, task common.WorkbenchTask) (common.WorkbenchTaskReceipt, error) {
	var empty common.WorkbenchTaskReceipt
	if (input.State != "done" && input.State != "open") || input.Title != "" || input.NotePath != "" || len(input.HeadingPath) != 0 {
		return empty, common.Err("INVALID_ARGUMENT", "Provide an explicit open or done state")
	}
	if !connection.WriteEnabled || s.workbenchActive[connection.ID] != nil {
		return empty, common.Err("CONFLICT", "Enable Obsidian writes and wait for the active synchronization to finish")
	}
	conflict := common.WorkbenchTaskReceipt{
		RequestID: input.RequestID, ConnectionID: connection.ID, SourceObjectID: task.ID, SyncState: "conflict",
	}
	if input.TargetVersion != workbenchFingerprint(task) {
		return conflict, nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	tasks, err := s.readWorkbench(ctx, connection.WorkbenchConnection)
	if err != nil {
		return empty, common.Err("CONFLICT", "Could not verify task identity in the vault")
	}
	key, _, _ := strings.Cut(task.SourceKey, "\x00")
	matches := 0
	for _, candidate := range tasks {
		if candidate.SourceKey == key || task.NativeID != "" && candidate.NativeID == task.NativeID {
			matches++
		}
	}
	if matches != 1 {
		return conflict, nil
	}
	path := task.NativeFields["path"]
	content, err := integrations.ReadWorkbenchObsidianNote(connection.WorkbenchConnection, path)
	if err != nil {
		return empty, common.Err("CONFLICT", "Could not read the task note")
	}
	replacement, err := integrations.SetWorkbenchObsidianCheckbox(connection.WorkbenchConnection, content, task, input.State)
	if err != nil {
		if problem, ok := err.(*common.Error); ok && problem.Code == "STALE_REVISION" {
			return conflict, nil
		}
		return empty, err
	}
	return s.writeWorkbenchObsidianNote(store, writer, connection, input, fingerprint, path, content, replacement)
}
