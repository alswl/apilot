package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchTaskWriteStore interface {
	StageWorkbenchTaskWrite(string, common.WorkbenchTaskReceipt) error
	SetWorkbenchTaskWriteState(common.WorkbenchTaskReceipt, string) error
	WorkbenchTaskReceipt(string) (common.WorkbenchTaskReceipt, string, error)
	CommitWorkbenchTaskWrite(common.WorkbenchConnection, []common.WorkbenchAttempt, []common.WorkbenchTask, []common.WorkbenchTask, time.Time, string, common.WorkbenchTaskReceipt) error
}

func workbenchFingerprint(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// workbenchTaskTargetVersion excludes mutable location details when a native ID
// identifies an Obsidian task. Source fields and the checkbox remain guarded.
func workbenchTaskTargetVersion(task common.WorkbenchTask) string {
	task.WorkbenchLabels = nil
	if task.Kind == common.WorkbenchObsidianTasks && task.NativeID != "" {
		task.Locator = ""
		task.Activity = nil
		task.NativeFields = maps.Clone(task.NativeFields)
		for _, key := range []string{"path", "file", "line", "section", "heading_path"} {
			delete(task.NativeFields, key)
		}
	}
	return workbenchFingerprint(task)
}

func workbenchTaskMatchesTarget(task common.WorkbenchTask, target string) bool {
	return target == workbenchTaskTargetVersion(task) || target == workbenchFingerprint(task)
}

func (s *Service) writeWorkbenchTaskLegacy(input common.WorkbenchTaskWrite) (common.WorkbenchTaskReceipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var empty common.WorkbenchTaskReceipt
	if s.stopping {
		return empty, common.Err("CONFLICT", "App is stopping")
	}
	if strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 256 || input.ConnectionVersion < 1 {
		return empty, common.Err("INVALID_ARGUMENT", "Request ID and connection version are required")
	}
	writer, ok := s.store.(workbenchTaskWriteStore)
	if !ok {
		return empty, common.Err("INTERNAL", "Task write storage unavailable")
	}
	store, err := s.workbenchStore()
	if err != nil {
		return empty, err
	}
	fingerprint := workbenchFingerprint(input)
	receipt, previousFingerprint, err := writer.WorkbenchTaskReceipt(input.RequestID)
	if err != nil {
		return empty, err
	}
	if previousFingerprint != "" {
		if previousFingerprint != fingerprint {
			return empty, common.Err("CONFLICT", "Request ID was already used with different input")
		}
		connection, err := s.WorkbenchSnapshot(receipt.ConnectionID)
		if err != nil {
			return empty, err
		}
		if err := s.workbenchPluginAvailable(connection.Kind); err != nil {
			return empty, err
		}
		if connection.Kind == common.WorkbenchObsidianTasks {
			if connection.Version != input.ConnectionVersion || connection.Paused || !connection.WriteEnabled {
				return empty, common.Err("CONFLICT", "Connection is not writable with this configuration")
			}
			if receipt.SyncState == "needs_review" {
				return s.reconcileObsidianWrite(store, writer, connection, input, fingerprint, receipt)
			}
		}
		return receipt, nil
	}
	var task common.WorkbenchTask
	connectionID := input.ConnectionID
	if input.SourceObjectID != "" {
		task, err = store.WorkbenchObject(input.SourceObjectID)
		if err != nil {
			return empty, err
		}
		if connectionID != "" && connectionID != task.ConnectionID {
			return empty, common.Err("INVALID_ARGUMENT", "Task belongs to another connection")
		}
		connectionID = task.ConnectionID
	}
	connection, err := s.WorkbenchSnapshot(connectionID)
	if err != nil {
		return empty, err
	}
	if err := s.workbenchPluginAvailable(connection.Kind); err != nil {
		return empty, err
	}
	if connection.Paused || connection.Version != input.ConnectionVersion {
		return empty, common.Err("CONFLICT", "Connection is not writable with this configuration")
	}
	if (input.SourceObjectID == "" && !connection.Capabilities.Create) || (input.SourceObjectID != "" && !connection.Capabilities.SetState) {
		return empty, common.Err("CONFLICT", "Connector does not support this task write")
	}
	return s.workbenchPlugins[connection.Kind].write(s, store, writer, connection, input, fingerprint, task)
}

func (s *Service) writeLocalWorkbenchTask(_ workbenchStore, writer workbenchTaskWriteStore, connection common.WorkbenchSnapshot, input common.WorkbenchTaskWrite, fingerprint string, task common.WorkbenchTask) (common.WorkbenchTaskReceipt, error) {
	var empty common.WorkbenchTaskReceipt
	var before []common.WorkbenchTask
	receipt := common.WorkbenchTaskReceipt{RequestID: input.RequestID, ConnectionID: connection.ID, SyncState: "confirmed"}
	now := time.Now().UTC()
	attempt := common.WorkbenchAttempt{ID: now.UnixNano(), Trigger: "write", State: "success", Started: now, Finished: &now}
	if input.SourceObjectID == "" {
		title := strings.TrimSpace(input.Title)
		if title == "" || len(title) > 1024 || strings.ContainsAny(input.Title, "\r\n\x00") || input.State != "" || input.NotePath != "" || len(input.HeadingPath) != 0 || input.TargetVersion != "" {
			return empty, common.Err("INVALID_ARGUMENT", "Provide a single-line Local task title")
		}
		key := common.ID()
		task = common.WorkbenchTask{
			ID: common.ID(), ConnectionID: connection.ID, Kind: connection.Kind, Mode: connection.Mode,
			SourceKey: key + "\x000", NativeID: key, Title: title, State: "open", NativeFields: map[string]string{"status": "open"},
		}
		attempt.Added = 1
	} else {
		if (input.State != "open" && input.State != "done") || input.Title != "" || input.NotePath != "" || len(input.HeadingPath) != 0 {
			return empty, common.Err("INVALID_ARGUMENT", "Provide an explicit open or done state")
		}
		if input.TargetVersion != workbenchFingerprint(task) {
			receipt.SyncState = "conflict"
			receipt.SourceObjectID = task.ID
			return receipt, nil
		}
		if task.State != input.State {
			attempt.Updated = 1
		}
		before = []common.WorkbenchTask{task}
		task.State = input.State
		recordWorkbenchStateChange(before[0], &task, now)
		fields := make(map[string]string, len(task.NativeFields)+1)
		for key, value := range task.NativeFields {
			fields[key] = value
		}
		fields["status"] = input.State
		task.NativeFields = fields
	}
	receipt.SourceObjectID = task.ID
	attempts := append([]common.WorkbenchAttempt{attempt}, connection.Attempts...)
	return receipt, writer.CommitWorkbenchTaskWrite(connection.WorkbenchConnection, attempts[:min(20, len(attempts))],
		before, []common.WorkbenchTask{task}, now, fingerprint, receipt)
}
