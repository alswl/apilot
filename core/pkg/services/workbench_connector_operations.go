package services

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type managedOperationStore interface {
	AdmitWorkbenchConnectorOperation(common.WorkbenchTaskWrite, string, string, common.WorkbenchConnectorOperationBinding, *common.WorkbenchFieldConflict) (common.WorkbenchTaskOperation, bool, error)
	RecordWorkbenchConnectorEvidence(string, protocol.MutationEvidence) error
	CommitWorkbenchConnectorOperation(common.WorkbenchConnectorSyncCommit, string, common.WorkbenchTask, common.WorkbenchTaskReceipt) (common.WorkbenchTaskReceipt, error)
}

func (s *Service) WorkbenchEditMetadata(ctx context.Context, taskID, scopeID string) (protocol.EditMetadataResult, error) {
	result, err := s.workbenchEditMetadata(ctx, taskID, scopeID)
	return result.EditMetadataResult, err
}

func (s *Service) workbenchEditMetadata(ctx context.Context, taskID, scopeID string) (protocol.ConnectorEditMetadataResult, error) {
	result := protocol.ConnectorEditMetadataResult{EditMetadataResult: protocol.EditMetadataResult{Fields: []protocol.FieldDefinition{}, Actions: []protocol.StateAction{}}}
	var task common.WorkbenchTask
	var err error
	if taskID != "" {
		task, err = s.WorkbenchTask(taskID)
		if err != nil {
			return result, err
		}
		scopeID = task.ConnectionID
	}
	connection, err := s.WorkbenchSnapshot(scopeID)
	if err != nil {
		return result, err
	}
	if connection.Paused || connection.PluginDisabled {
		return result, nil
	}
	if connection.Kind == common.WorkbenchLocal {
		result.Create = taskID == ""
		for _, id := range []string{"title", "description", "assignees", "priority", "due_date", "labels"} {
			result.Fields = append(result.Fields, protocol.FieldDefinition{ID: id, Label: id, Type: workbenchMappingTypes[id], Writable: true})
		}
		if taskID != "" {
			for _, state := range []string{"open", "done"} {
				if state != task.State {
					result.Actions = append(result.Actions, protocol.StateAction{ID: state, Label: state})
				}
			}
		}
		return result, nil
	}
	if connection.ParentConnectionID == "" || !connection.WriteEnabled {
		return result, nil
	}
	scope, process, err := s.readableConnectorScope(scopeID)
	if err != nil {
		return result, err
	}
	meta, err := callWorkbenchConnector[protocol.ConnectorEditMetadataResult](ctx, process, common.ID(), protocol.EditMetadata,
		protocol.DetailParams{ScopeContext: scope.Context, SourceID: task.SourceKey, Number: task.NativeID}, nil)
	if err != nil {
		return result, publicConnectorError(err)
	}
	catalog, err := s.WorkbenchConnectorFields(ctx, scopeID)
	if err != nil {
		return result, err
	}
	rules := effectiveConnectorRules(catalog, scope)
	issues := connectorMappingIssues(catalog, rules)
	for _, field := range meta.Fields {
		for target, rule := range rules {
			if rule.Write && rule.Field == field.ID && field.Writable && issues[target] == "" {
				field.ID = target
				result.Fields = append(result.Fields, field)
				break
			}
		}
	}
	result.Actions, result.Create, result.Observation = meta.Actions, meta.Create, meta.Observation
	return result, nil
}

func effectiveConnectorRules(catalog protocol.FieldCatalog, scope common.WorkbenchConnectorStoredScope) map[string]protocol.MappingRule {
	rules := maps.Clone(catalog.Defaults)
	if rules == nil {
		rules = map[string]protocol.MappingRule{}
	}
	maps.Copy(rules, scope.Mapping)
	return rules
}

func validWorkbenchEditField(id string, field protocol.FieldValue) bool {
	if !protocol.ValidFieldValue(field) || field.Type != workbenchMappingTypes[id] {
		return false
	}
	if id == "due_date" && string(field.Value) != "null" {
		var raw string
		if json.Unmarshal(field.Value, &raw) != nil {
			return false
		}
		_, err := time.Parse(time.DateOnly, raw)
		return err == nil
	}
	if id == "title" {
		var title string
		return json.Unmarshal(field.Value, &title) == nil && strings.TrimSpace(title) != "" && len(title) <= 1024 && !strings.ContainsAny(title, "\r\n\x00")
	}
	return true
}

func (s *Service) submitConnectorOperation(input common.WorkbenchTaskWrite, connection common.WorkbenchSnapshot) (common.WorkbenchTaskReceipt, error) {
	var empty common.WorkbenchTaskReceipt
	ops, ok := s.store.(managedOperationStore)
	if !ok {
		return empty, common.Err("INTERNAL", "Connector operation storage unavailable")
	}
	scope, process, err := s.readableConnectorScope(connection.ID)
	if err != nil {
		return empty, err
	}
	if scope.ConnectorID != "github-issues" && scope.ConnectorID != "fixture" {
		return empty, common.Err("FORBIDDEN", "Connector is read-only")
	}
	if !scope.Config.WriteEnabled || scope.Config.Paused || scope.Config.Version != input.ConnectionVersion ||
		(input.MappingRevision != nil && *input.MappingRevision != scope.MappingRevision) {
		return empty, common.Err("STALE_REVISION", "Scope write settings changed")
	}
	if input.NotePath != "" || len(input.HeadingPath) > 0 || (len(input.Fields) > 0 && (input.Action != "" || input.State != "" || input.Title != "" || input.Body != "")) ||
		(input.Action != "" && (input.State != "" || input.Title != "" || input.Body != "")) ||
		(input.Action == "" && len(input.ActionFields) > 0) {
		return empty, common.Err("INVALID_ARGUMENT", "Submit one field patch, creation, or state action")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 25*time.Second)
	defer cancel()
	mutation := protocol.MutationParams{ScopeContext: scope.Context, OperationID: input.RequestID, Action: input.Action, ActionFields: input.ActionFields}
	var task common.WorkbenchTask
	var conflict *common.WorkbenchFieldConflict
	var staleTask bool
	if input.SourceObjectID != "" {
		task, err = s.WorkbenchTask(input.SourceObjectID)
		if err != nil {
			return empty, err
		}
		if task.ConnectionID != connection.ID {
			return empty, common.Err("INVALID_ARGUMENT", "Task belongs to another scope")
		}
		mutation.SourceID, mutation.Number, mutation.ExpectedVersion = task.SourceKey, task.NativeID, task.NativeFields["source_version"]
		if mutation.Action == "" && input.State != "" && scope.ConnectorID == "github-issues" {
			switch input.State {
			case "open":
				mutation.Action = "reopen"
			case "done":
				mutation.Action = "close"
			}
		}
		staleTask = task.TargetVersion != input.TargetVersion
		if staleTask && input.TargetVersion == "" {
			return empty, common.Err("STALE_REVISION", "Task changed before operation admission")
		}
		if staleTask && mutation.Action != "" {
			state := task.NativeFields["state"]
			if state == "" {
				state = task.State
			}
			current := map[string]protocol.FieldValue{"state": protocol.Value(protocol.FieldText, state)}
			var reason protocol.FieldValue
			if json.Unmarshal([]byte(task.NativeFields["native.state_reason"]), &reason) == nil && protocol.ValidFieldValue(reason) {
				current["state_reason"] = reason
			}
			conflict = connectorOperationConflict(mutation, current, nil)
		}
	}
	if conflict == nil {
		if mutation.Action == "" && len(input.Fields) == 0 && input.SourceObjectID != "" {
			return empty, common.Err("INVALID_ARGUMENT", "Provide fields or a supported action")
		}
		meta, metaErr := s.workbenchEditMetadata(ctx, input.SourceObjectID, connection.ID)
		if metaErr != nil {
			return empty, metaErr
		}
		if mutation.Action != "" {
			if !connectorActionValid(meta.EditMetadataResult, mutation.Action, mutation.ActionFields) {
				observation := meta.Observation
				if mutation.ExpectedVersion == "" || observation == nil || observation.Version == "" ||
					observation.Version == mutation.ExpectedVersion || !protocol.ValidFieldValue(observation.Fields["state"]) ||
					slices.ContainsFunc(meta.Actions, func(action protocol.StateAction) bool { return action.ID == mutation.Action }) {
					return empty, common.Err("INVALID_ARGUMENT", "Action is unavailable or required fields are missing")
				}
				conflict = connectorOperationConflict(mutation, observation.Fields, &task)
			}
		} else {
			if input.SourceObjectID == "" && !meta.Create {
				return empty, common.Err("CONFLICT", "Connector cannot create tasks in this scope")
			}
			fields := input.Fields
			if input.SourceObjectID == "" && input.Title != "" {
				fields = map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, input.Title)}
				if input.Body != "" {
					fields["description"] = protocol.Value(protocol.FieldRichText, input.Body)
				}
			}
			catalog, err := s.WorkbenchConnectorFields(ctx, scope.ScopeID)
			if err != nil {
				return empty, err
			}
			rules := effectiveConnectorRules(catalog, scope)
			patch := make(map[string]protocol.FieldValue)
			baseline := make(map[string]protocol.FieldValue)
			for id, value := range fields {
				rule := rules[id]
				if !connectorWritableField(meta.EditMetadataResult, id) || !validWorkbenchEditField(id, value) || rule.Field == "" {
					return empty, common.Err("INVALID_ARGUMENT", "Field is read only or its value is invalid: "+id)
				}
				patch[rule.Field] = value
				if input.SourceObjectID != "" {
					var existing protocol.FieldValue
					if json.Unmarshal([]byte(task.NativeFields["native."+rule.Field]), &existing) == nil {
						baseline[rule.Field] = existing
					}
				}
			}
			if len(patch) == 0 {
				return empty, common.Err("INVALID_ARGUMENT", "Provide at least one field")
			}
			if input.SourceObjectID == "" {
				mutation.Create = patch
			} else {
				mutation.Patch = patch
				if staleTask || len(baseline) != len(patch) {
					page, err := callWorkbenchConnector[protocol.DetailPage](ctx, process, common.ID(), protocol.TaskDetail,
						protocol.DetailParams{ScopeContext: scope.Context, SourceID: task.SourceKey, Number: task.NativeID, Segment: "fields"}, nil)
					if err != nil {
						return empty, publicConnectorError(err)
					}
					if page.Version == "" {
						return empty, publicConnectorError(integrations.ErrWorkbenchConnectorProtocol)
					}
					if staleTask || page.Version != mutation.ExpectedVersion {
						conflict = connectorOperationConflict(mutation, page.Fields, nil)
					} else {
						for id := range patch {
							if _, ok := baseline[id]; !ok {
								field, ok := page.Fields[id]
								if !ok {
									return empty, common.Err("CONFLICT", "Field baseline is unavailable: "+id)
								}
								baseline[id] = field
							}
						}
					}
				}
				mutation.Baseline = baseline
			}
		}
	}

	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return empty, common.Err("CONFLICT", "App is stopping")
	}
	current, _, err := s.readableConnectorScope(connection.ID)
	if err != nil {
		s.mu.Unlock()
		return empty, err
	}
	if current.SnapshotVersion != scope.SnapshotVersion || current.AuthGeneration != scope.AuthGeneration || current.ConfigRevision != scope.ConfigRevision || current.MappingRevision != scope.MappingRevision {
		s.mu.Unlock()
		return empty, common.Err("STALE_REVISION", "Scope changed before operation admission")
	}
	op, inserted, err := ops.AdmitWorkbenchConnectorOperation(input, connection.ID, workbenchFingerprint(input), common.WorkbenchConnectorOperationBinding{
		Mutation: mutation, ConfigRevision: scope.ConfigRevision, MappingRevision: scope.MappingRevision, AuthGeneration: scope.AuthGeneration,
		PackageDigest: s.workbenchConnectorRelease.PackageDigest,
	}, conflict)
	if err == nil && inserted && conflict != nil {
		s.volatileRevision.Add(1)
	}
	s.mu.Unlock()
	if err == nil && op.Phase == common.WorkbenchOperationAccepted {
		s.scheduleWorkbenchOperation(input.RequestID)
	}
	return op.Receipt, err
}

func connectorWritableField(meta protocol.EditMetadataResult, id string) bool {
	for _, f := range meta.Fields {
		if f.ID == id && f.Writable {
			return true
		}
	}
	return false
}

func connectorActionValid(meta protocol.EditMetadataResult, id string, fields map[string]protocol.FieldValue) bool {
	for _, action := range meta.Actions {
		if action.ID != id {
			continue
		}
		if len(fields) != len(action.Required) {
			return false
		}
		for _, required := range action.Required {
			field := fields[required.ID]
			if field.Type != required.Type || !protocol.ValidFieldValue(field) || string(field.Value) == "null" || string(field.Value) == `""` {
				return false
			}
		}
		return true
	}
	return false
}

func (s *Service) runConnectorOperation(ctx context.Context, op common.WorkbenchTaskOperation, reconcile bool) (common.WorkbenchTaskReceipt, error) {
	receipt := op.Receipt
	from := common.WorkbenchOperationAccepted
	if reconcile {
		from = common.WorkbenchOperationNeedsReview
	}
	if op.Phase != from {
		return receipt, nil
	}
	ops := s.store.(workbenchTaskOperationStore)
	managed := s.store.(managedOperationStore)
	binding := op.Binding.Connector
	releaseGate, err := s.operationNotes.lock(ctx, "connector:"+receipt.ConnectionID)
	if err != nil {
		return receipt, err
	}
	defer releaseGate()
	s.mu.Lock()
	scope, process, err := s.readableConnectorScope(receipt.ConnectionID)
	valid := err == nil && scope.ConfigRevision == binding.ConfigRevision && scope.MappingRevision == binding.MappingRevision &&
		scope.AuthGeneration == binding.AuthGeneration && s.workbenchConnectorRelease.PackageDigest == binding.PackageDigest
	if !valid || (!reconcile && (!scope.Config.WriteEnabled || scope.Config.Paused || s.stopping || (scope.ConnectorID != "github-issues" && scope.ConnectorID != "fixture"))) {
		if !reconcile {
			receipt.SyncState, receipt.Reason = common.WorkbenchOperationNotExecuted, "Scope access or mapping changed before dispatch"
			err = ops.AdvanceWorkbenchTaskOperation(receipt.RequestID, from, receipt.SyncState, receipt)
		}
		s.mu.Unlock()
		if reconcile {
			return receipt, nil
		}
		return receipt, err
	}
	if !reconcile {
		claimed, claimErr := ops.ClaimWorkbenchTaskOperation(receipt.RequestID)
		if claimErr != nil {
			s.mu.Unlock()
			return receipt, claimErr
		}
		receipt = claimed.Receipt
		from = common.WorkbenchOperationDispatching
		scope.SnapshotVersion++
	}
	release := *s.workbenchConnectorRelease
	s.mu.Unlock()
	request := binding.Mutation
	if reconcile && receipt.Evidence != nil {
		request.SourceID, request.Number = receipt.Evidence.SourceID, receipt.Evidence.Number
	}
	method := protocol.Mutate
	if reconcile {
		method = protocol.Reconcile
	}
	result, err := callWorkbenchConnector[protocol.MutationResult](ctx, process, common.ID(), method, request, func(msg protocol.Message) error {
		if msg.Method != "operation.evidence" || reconcile || len(request.Create) == 0 {
			return nil
		}
		var evidence protocol.MutationEvidence
		if json.Unmarshal(msg.Params, &evidence) != nil || evidence.SourceID == "" || evidence.Number == "" {
			return integrations.ErrWorkbenchConnectorProtocol
		}
		if err := managed.RecordWorkbenchConnectorEvidence(receipt.RequestID, evidence); err != nil {
			return err
		}
		receipt.Evidence = &evidence
		return nil
	})
	if result.Evidence != nil && len(request.Create) > 0 && result.Evidence.SourceID != "" && result.Evidence.Number != "" {
		receipt.Evidence = result.Evidence
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil && result.Status == "confirmed" && result.Record != nil {
		match := request.SourceID == result.Record.SourceID
		if request.SourceID == "" && receipt.Evidence != nil {
			match = receipt.Evidence.SourceID == result.Record.SourceID
		}
		if match {
			task, projectionErr := connectorTask(scope, *result.Record)
			if projectionErr == nil {
				commit := common.WorkbenchConnectorSyncCommit{Scope: scope, Release: release, PluginID: s.managedPluginID(scope.ConnectorID)}
				saved, commitErr := managed.CommitWorkbenchConnectorOperation(commit, from, task, receipt)
				if commitErr == nil {
					s.volatileRevision.Add(1)
					return saved, nil
				}
			}
		}
	}
	switch {
	case err == nil && result.Status == "conflict":
		receipt.SyncState, receipt.Reason = common.WorkbenchOperationConflict, "Source changed since the operation was prepared"
		receipt.Conflict = connectorOperationConflict(request, result.Current, op.Binding.Baseline)
	case err == nil && result.Status == "not_executed":
		receipt.SyncState, receipt.Reason = common.WorkbenchOperationNotExecuted, "Source rejected the operation before writing"
	default:
		receipt.SyncState, receipt.Reason = common.WorkbenchOperationNeedsReview, "Source confirmation is unavailable"
		if reconcile {
			return receipt, nil
		}
	}
	if err := ops.AdvanceWorkbenchTaskOperation(receipt.RequestID, from, receipt.SyncState, receipt); err != nil {
		return op.Receipt, err
	}
	s.volatileRevision.Add(1)
	return receipt, nil
}

func connectorOperationConflict(request protocol.MutationParams, current map[string]protocol.FieldValue, baseline *common.WorkbenchTask) *common.WorkbenchFieldConflict {
	conflict := &common.WorkbenchFieldConflict{Action: request.Action, Proposed: request.Patch, Current: make(map[string]protocol.FieldValue)}
	if request.Action != "" {
		conflict.Proposed = request.ActionFields
		if baseline != nil {
			state := protocol.Value(protocol.FieldText, baseline.NativeFields["state"])
			conflict.PreviousState = &state
		}
		for _, id := range []string{"state", "state_reason"} {
			if field, ok := current[id]; ok {
				conflict.Current[id] = field
			}
		}
	}
	for id := range conflict.Proposed {
		if field, ok := current[id]; ok {
			conflict.Current[id] = field
		}
	}
	return conflict
}
