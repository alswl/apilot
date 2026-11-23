package services

import (
	"context"
	"encoding/json"
	"maps"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

func (s *Service) WorkbenchConnectorFields(ctx context.Context, id string) (protocol.FieldCatalog, error) {
	scope, process, err := s.readableConnectorScope(id)
	if err != nil {
		return protocol.FieldCatalog{}, err
	}
	catalog, err := callWorkbenchConnector[protocol.FieldCatalog](ctx, process, common.ID(), protocol.FieldsDescribe, scope.Context, nil)
	return catalog, publicConnectorError(err)
}

func (s *Service) WorkbenchConnectorMapping(ctx context.Context, id string, input *common.WorkbenchMappingInput, save bool) (common.WorkbenchMappingResult, error) {
	var output common.WorkbenchMappingResult
	scope, process, err := s.readableConnectorScope(id)
	if err != nil {
		return output, err
	}
	catalog, err := callWorkbenchConnector[protocol.FieldCatalog](ctx, process, common.ID(), protocol.FieldsDescribe, scope.Context, nil)
	if err != nil {
		return output, publicConnectorError(err)
	}
	output.ScopeID, output.Revision = id, scope.MappingRevision
	rules := maps.Clone(catalog.Defaults)
	if rules == nil {
		rules = make(map[string]protocol.MappingRule)
	}
	if input == nil || !input.RestoreDefaults {
		maps.Copy(rules, scope.Mapping)
	}
	if input != nil {
		if input.Revision != scope.MappingRevision {
			return output, common.Err("STALE_REVISION", "Scope mapping changed")
		}
		if input.RestoreDefaults && len(input.Rules) > 0 {
			return output, common.Err("INVALID_ARGUMENT", "Choose default mapping or rules")
		}
		maps.Copy(rules, input.Rules)
	}
	output.Rules = rules
	output.Issues = connectorMappingIssues(catalog, rules)
	if input == nil {
		return output, nil
	}
	if input.TaskID != "" {
		sample, err := s.WorkbenchTask(input.TaskID)
		if err != nil {
			return output, err
		}
		if sample.ConnectionID != id {
			return output, common.Err("INVALID_ARGUMENT", "Sample task belongs to another scope")
		}
		output.PendingRead = applyConnectorMapping(&sample, rules)
		if output.PendingRead && len(output.Issues) == 0 {
			page, err := callWorkbenchConnector[protocol.DetailPage](ctx, process, common.ID(), protocol.TaskDetail,
				protocol.DetailParams{ScopeContext: scope.Context, SourceID: sample.SourceKey, Number: sample.NativeID, Segment: "fields"}, nil)
			if err == nil && page.Version == sample.NativeFields["source_version"] {
				sample.NativeFields = maps.Clone(sample.NativeFields)
				for key, value := range page.Fields {
					raw, _ := json.Marshal(value)
					sample.NativeFields["native."+key] = string(raw)
				}
				output.PendingRead = applyConnectorMapping(&sample, rules)
			}
		}
		output.Sample = &sample
	}
	if !save || len(output.Issues) > 0 {
		return output, nil
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return output, err
	}
	writer, ok := store.(interface {
		SaveWorkbenchConnectorMapping(common.WorkbenchConnectorStoredScope, map[string]protocol.MappingRule, []common.WorkbenchTask) error
	})
	if !ok {
		return output, common.Err("INTERNAL", "Mapping storage unavailable")
	}
	tasks, err := store.WorkbenchConnectorMembers(id)
	if err != nil {
		return output, err
	}
	for i := range tasks {
		pending := applyConnectorMapping(&tasks[i], rules)
		output.PendingRead = output.PendingRead || pending
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return output, common.Err("CONFLICT", "App is stopping")
	}
	if err := writer.SaveWorkbenchConnectorMapping(scope, rules, tasks); err != nil {
		return output, err
	}
	output.Revision++
	s.volatileRevision.Add(1)
	if output.PendingRead && s.workbenchActive[id] == nil {
		_, _ = s.startWorkbenchConnectorRunLocked(store, id, "mapping", true)
	}
	return output, nil
}

var workbenchMappingTypes = map[string]protocol.FieldType{
	"title": protocol.FieldText, "description": protocol.FieldRichText, "assignees": protocol.FieldAccounts,
	"priority": protocol.FieldNumber, "due_date": protocol.FieldDate, "labels": protocol.FieldLabels, "state": protocol.FieldText,
}

func connectorMappingIssues(catalog protocol.FieldCatalog, rules map[string]protocol.MappingRule) map[string]string {
	issues := make(map[string]string)
	fields := make(map[string]protocol.FieldDefinition)
	for _, field := range catalog.Fields {
		fields[field.ID] = field
	}
	written := make(map[string]string)
	for target, rule := range rules {
		want, ok := workbenchMappingTypes[target]
		field := fields[rule.Field]
		switch {
		case !ok || field.ID == "":
			issues[target] = "unknown_field"
		case field.Type != want:
			issues[target] = "incompatible_type"
		case rule.Write && (!field.Writable || target == "state"):
			issues[target] = "read_only"
		case target == "state":
			for _, v := range rule.Values {
				if !validWorkbenchState(v) {
					issues[target] = "invalid_state"
					break
				}
			}
		}
		if rule.Write {
			if other := written[rule.Field]; other != "" {
				issues[target] = "ambiguous_write"
				issues[other] = "ambiguous_write"
			}
			written[rule.Field] = target
		}
	}
	return issues
}

func applyConnectorMapping(task *common.WorkbenchTask, rules map[string]protocol.MappingRule) bool {
	pending := false
	for target, rule := range rules {
		if target == "description" {
			continue
		}
		var field protocol.FieldValue
		value, ok := task.NativeFields["native."+rule.Field]
		if !ok || json.Unmarshal([]byte(value), &field) != nil || field.Type != workbenchMappingTypes[target] {
			pending = true
			if target == "due_date" {
				task.DueDate = ""
			}
			if target == "priority" {
				task.Priority = nil
			}
			continue
		}
		switch target {
		case "title":
			var title string
			if json.Unmarshal(field.Value, &title) == nil && title != "" {
				task.Title = title
			}
		case "assignees":
			var values []protocol.Option
			if json.Unmarshal(field.Value, &values) == nil {
				task.Assignees = nil
				for _, value := range values {
					task.Assignees = append(task.Assignees, value.Label)
				}
			}
		case "labels":
			task.Labels = nil
			_ = json.Unmarshal(field.Value, &task.Labels)
		case "priority":
			var value *int
			task.Priority = nil
			if json.Unmarshal(field.Value, &value) == nil {
				task.Priority = value
			}
		case "due_date":
			task.DueDate = ""
			var value string
			if json.Unmarshal(field.Value, &value) == nil {
				if _, err := time.Parse(time.DateOnly, value); err == nil {
					task.DueDate = value
				}
			}
		case "state":
			var native string
			if json.Unmarshal(field.Value, &native) == nil {
				if value := rule.Values[native]; validWorkbenchState(value) {
					task.State = value
				}
			}
		}
	}
	return pending
}
