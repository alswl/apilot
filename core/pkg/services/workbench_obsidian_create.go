package services

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func (s *Service) writeObsidianWorkbenchTask(store workbenchStore, writer workbenchTaskWriteStore, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, fingerprint string, task common.WorkbenchTask) (common.WorkbenchTaskReceipt, error) {
	if input.SourceObjectID != "" {
		return s.setWorkbenchObsidianState(store, writer, connection, input, fingerprint, task)
	}
	return s.createWorkbenchObsidianTask(store, writer, connection, input, fingerprint)
}

func (s *Service) createWorkbenchObsidianTask(store workbenchStore, writer workbenchTaskWriteStore, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, fingerprint string) (common.WorkbenchTaskReceipt, error) {
	var empty common.WorkbenchTaskReceipt
	content, replacement, err := prepareWorkbenchObsidianCreation(connection.WorkbenchConnection, input, fingerprint)
	if err != nil {
		return empty, err
	}
	return s.writeWorkbenchObsidianNote(store, writer, connection, input, fingerprint, input.NotePath, content, replacement)
}

func prepareWorkbenchObsidianCreation(connection common.WorkbenchConnection, input common.WorkbenchTaskWrite, fingerprint string) ([]byte, []byte, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" || len(title) > 1024 || input.State != "" || len(fingerprint) < 32 {
		return nil, nil, common.Err("INVALID_ARGUMENT", "Provide a valid task title and request")
	}
	content, err := integrations.ReadWorkbenchObsidianNote(connection, input.NotePath)
	if err != nil {
		return nil, nil, common.Err("CONFLICT", "Could not read the selected note")
	}
	marker := obsidianCreationMarker(fingerprint)
	replacement, err := integrations.InsertWorkbenchObsidianTask(content, input.HeadingPath, input.TargetVersion, title, marker)
	if err != nil {
		return nil, nil, err
	}
	return content, replacement, nil
}

func (s *Service) writeWorkbenchObsidianNote(store workbenchStore, writer workbenchTaskWriteStore, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, fingerprint, path string, content, replacement []byte) (common.WorkbenchTaskReceipt, error) {
	var empty common.WorkbenchTaskReceipt
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	write, err := s.prepareWorkbenchObsidianNoteWrite(ctx, connection, path, content, replacement)
	if err != nil {
		return empty, err
	}
	receipt := common.WorkbenchTaskReceipt{RequestID: input.RequestID, ConnectionID: connection.ID, SyncState: "needs_review", SourceObjectID: input.SourceObjectID}
	if err := writer.StageWorkbenchTaskWrite(fingerprint, receipt); err != nil {
		return empty, err
	}
	if err := write(); err != nil {
		if problem, ok := err.(*common.Error); ok && problem.Code == "STALE_REVISION" {
			receipt.Reason = problem.Message
			if err := writer.SetWorkbenchTaskWriteState(receipt, "conflict"); err != nil {
				return receipt, err
			}
			receipt.SyncState = "conflict"
			return receipt, nil
		}
	}
	return s.reconcileObsidianWrite(store, writer, connection, input, fingerprint, receipt)
}

func (s *Service) prepareWorkbenchObsidianNoteWrite(ctx context.Context, connection common.WorkbenchSnapshot, path string, content, replacement []byte) (func() error, error) {
	if s.workbenchActive[connection.ID] != nil {
		return nil, common.Err("CONFLICT", "Wait for the active synchronization to finish")
	}
	return s.prepareWorkbenchObsidianNoteWriteUnlocked(ctx, connection, path, content, replacement)
}

// prepareWorkbenchObsidianNoteWriteUnlocked does source I/O without accessing
// the service's mutable scan state. Dispatch authorization is checked separately.
func (s *Service) prepareWorkbenchObsidianNoteWriteUnlocked(ctx context.Context, connection common.WorkbenchSnapshot, path string, content, replacement []byte) (func() error, error) {
	if connection.ReadMethod == common.WorkbenchObsidianCLI || !connection.BridgeConfigured {
		return integrations.PrepareWorkbenchObsidianCLIWrite(ctx, connection.WorkbenchConnection, path, content, replacement)
	}
	store, ok := s.store.(workbenchBridgeStore)
	if !ok {
		return nil, common.Err("INTERNAL", "Obsidian bridge settings unavailable")
	}
	settings, err := store.WorkbenchBridge(connection.ID)
	if err != nil {
		return nil, err
	}
	bridge, err := integrations.OpenWorkbenchObsidianBridge(settings.Endpoint, settings.Token, settings.Certificate)
	if err != nil {
		return nil, err
	}
	if err := bridge.VerifyVault(ctx, connection.WorkbenchConnection); err != nil {
		return nil, err
	}
	remote, etag, err := bridge.ReadNote(ctx, path)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(remote, content) {
		return nil, common.Err("STALE_REVISION", "Source note changed before writing")
	}
	return func() error { return bridge.ReplaceNote(ctx, path, replacement, etag) }, nil
}

func obsidianCreationMarker(fingerprint string) string {
	return "apilot-" + fingerprint[:32]
}

func (s *Service) reconcileObsidianWrite(store workbenchStore, writer workbenchTaskWriteStore, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, fingerprint string, receipt common.WorkbenchTaskReceipt) (common.WorkbenchTaskReceipt, error) {
	if s.workbenchActive[connection.ID] != nil {
		return receipt, nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	tasks, err := readWorkbenchObsidianWrittenVault(ctx, connection.WorkbenchConnection)
	if err != nil {
		return receipt, nil
	}
	before, err := store.WorkbenchObjects(connection.ID)
	if err != nil {
		return receipt, err
	}
	matchWorkbench(connection.WorkbenchConnection, before, tasks)
	now := time.Now().UTC()
	recordObsidianImport(connection.WorkbenchConnection, before, tasks, now)
	index := -1
	if input.SourceObjectID != "" {
		for i := range tasks {
			date := tasks[i].NativeFields["done"]
			if tasks[i].ID == input.SourceObjectID && tasks[i].State == input.State && (date != "") == (input.State == "done") {
				index = i
				break
			}
		}
	} else {
		marker := obsidianCreationMarker(fingerprint)
		heading, _ := json.Marshal(input.HeadingPath)
		for i := range tasks {
			if tasks[i].NativeID != marker {
				continue
			}
			if index >= 0 || tasks[i].NativeFields["path"] != input.NotePath || tasks[i].NativeFields["heading_path"] != string(heading) {
				return receipt, nil
			}
			index = i
		}
	}
	if index < 0 {
		return receipt, nil
	}
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), Trigger: "write", State: "success", Started: now, Finished: &now}
	countWorkbenchChanges(&attempt, before, tasks)
	attempts := append([]common.WorkbenchAttempt{attempt}, connection.Attempts...)
	confirmed := receipt
	confirmed.SyncState, confirmed.SourceObjectID = "confirmed", tasks[index].ID
	if err := writer.CommitWorkbenchTaskWrite(connection.WorkbenchConnection, attempts[:min(20, len(attempts))], before, tasks, now, fingerprint, confirmed); err != nil {
		return receipt, err
	}
	return confirmed, nil
}

// The CLI commits to the verified local vault. Read the resulting Markdown
// from disk when confirming writes, including when the CLI response was lost.
func readWorkbenchObsidianWrittenVault(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
	c.ReadMethod = ""
	return integrations.ReadWorkbenchObsidian(ctx, c)
}
