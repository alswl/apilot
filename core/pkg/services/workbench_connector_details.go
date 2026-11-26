package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type workbenchDetailStore interface {
	RevokeWorkbenchTaskDetail(common.WorkbenchConnectorStoredScope, string, string, string) error
	ReadWorkbenchTaskDetail(common.WorkbenchConnectorStoredScope, string, string, string, string, string) (common.WorkbenchTaskDetail, error)
	SaveWorkbenchTaskDetail(common.WorkbenchConnectorStoredScope, string, string, string, common.WorkbenchTaskDetail) error
}

func (s *Service) readableConnectorScope(id string) (common.WorkbenchConnectorStoredScope, *integrations.WorkbenchConnectorProcess, error) {
	store, err := s.managedConnectorStore()
	if err != nil {
		return common.WorkbenchConnectorStoredScope{}, nil, err
	}
	scope, err := store.WorkbenchConnectorScope(id)
	if err != nil {
		return scope, nil, err
	}
	if scope.Config.Paused || scope.AuthState != "authorized" || scope.AccessState != "available" {
		return scope, nil, common.Err("FORBIDDEN", "Connector scope is unavailable")
	}
	process, err := s.managedConnector(scope.ConnectorID)
	return scope, process, err
}

func (s *Service) WorkbenchConnectorTaskDetail(ctx context.Context, id, segment, page string, expectedScope ...string) (common.WorkbenchTaskDetail, error) {
	result := common.WorkbenchTaskDetail{TaskID: id, Segment: segment}
	switch segment {
	case "fields", "description", "comments", "attachments", "links", "history":
	default:
		return result, common.Err("INVALID_ARGUMENT", "Unknown detail segment")
	}
	task, err := s.WorkbenchTask(id)
	if err != nil {
		return result, err
	}
	if len(expectedScope) != 0 && expectedScope[0] != "" && expectedScope[0] != task.ConnectionID {
		return result, common.Err("FORBIDDEN", "Task belongs to a different scope")
	}
	result.ScopeID = task.ConnectionID
	if task.Kind == common.WorkbenchLocal || task.Kind == common.WorkbenchObsidianTasks || task.Kind == common.WorkbenchGitHubRepository || task.Kind == common.WorkbenchGitHubIssues {
		result.CapturedAt = time.Now().UTC()
		result.Version = task.TargetVersion
		switch segment {
		case "fields":
			result.Fields = map[string]protocol.FieldValue{}
			for k, v := range task.NativeFields {
				result.Fields[k] = protocol.Value(protocol.FieldText, v)
			}
		case "description":
			result.Items = []protocol.DetailItem{{ID: "summary", Label: "Description", Text: &protocol.RichText{Format: "markdown", Content: protocol.Value(protocol.FieldText, task.Summary).Value}}}
		}
		return result, nil
	}
	scope, process, err := s.readableConnectorScope(task.ConnectionID)
	if err != nil {
		return result, err
	}
	store, ok := s.store.(workbenchDetailStore)
	if !ok {
		return result, common.Err("INTERNAL", "Task detail storage unavailable")
	}
	version := task.NativeFields["source_version"]
	connector := s.registeredConnector(scope.ConnectorID)
	if connector.process != process {
		return result, common.Err("CONFLICT", "Connector release changed")
	}
	digest := connector.release.PackageDigest
	sourceSegment := segment
	mappedDescription := scope.Mapping["description"].Field
	if segment == "description" && mappedDescription != "" {
		sourceSegment = "fields"
		digest += ":mapping:" + strconv.FormatInt(scope.MappingRevision, 10)
	}
	body, err := callWorkbenchConnector[protocol.DetailPage](ctx, process, common.ID(), protocol.TaskDetail,
		protocol.DetailParams{ScopeContext: scope.Context, SourceID: task.SourceKey, Number: task.NativeID, Segment: sourceSegment, Page: page}, nil)
	if err != nil {
		if current, currentErr := s.managedConnector(scope.ConnectorID); currentErr != nil || current != process {
			return result, common.Err("CONFLICT", "Connector availability changed")
		}
		var problem *protocol.RPCError
		if errors.As(err, &problem) && (problem.Data.Kind == protocol.AuthRequired || problem.Data.Kind == protocol.AccessDenied) {
			level := problem.Data.AffectedLevel
			if problem.Data.Kind == protocol.AuthRequired {
				level = "account"
			}
			s.mu.Lock()
			revokeErr := store.RevokeWorkbenchTaskDetail(scope, id, version, level)
			if revokeErr == nil {
				s.volatileRevision.Add(1)
			}
			s.mu.Unlock()
			if revokeErr != nil {
				return result, revokeErr
			}
		}
		switch connectorErrorKind(err) {
		case string(protocol.Temporary), common.WorkbenchConnectorTimeout, common.WorkbenchConnectorProcessFailure:
			if cached, cacheErr := store.ReadWorkbenchTaskDetail(scope, id, version, digest, segment, page); cacheErr == nil {
				return cached, nil
			}
		}
		return result, publicConnectorError(err)
	}
	if body.Version == "" || (body.NextPage != "" && body.NextPage == page) {
		return result, publicConnectorError(integrations.ErrWorkbenchConnectorProtocol)
	}
	if sourceSegment != segment {
		field, ok := body.Fields[mappedDescription]
		if !ok || field.Type != protocol.FieldRichText || !protocol.ValidFieldValue(field) || body.NextPage != "" {
			return result, common.Err("CONFLICT", "Mapped description is unavailable")
		}
		body.Fields = nil
		body.Items = []protocol.DetailItem{{ID: mappedDescription, Label: "Description", Text: &protocol.RichText{Format: "markdown", Content: field.Value}}}
	}
	for i := range body.Items {
		if body.Items[i].URL != "" {
			target, err := url.Parse(body.Items[i].URL)
			if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil {
				body.Items[i].URL = ""
			}
		}
	}
	result.Version, result.Fields, result.Items, result.NextPage = body.Version, body.Fields, body.Items, body.NextPage
	if segment == "fields" {
		result.TopFields = connectorTaskTopFields(connector.description.TaskTopFields, body.Fields)
	}
	result.CapturedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, currentErr := s.managedConnector(scope.ConnectorID); currentErr != nil || current != process {
		return result, common.Err("CONFLICT", "Connector availability changed")
	}
	if err := store.SaveWorkbenchTaskDetail(scope, version, digest, page, result); err != nil {
		return common.WorkbenchTaskDetail{}, err
	}
	return result, nil
}

func connectorTaskTopFields(declarations []protocol.TaskTopField, fields map[string]protocol.FieldValue) []common.WorkbenchTaskTopField {
	var result []common.WorkbenchTaskTopField
	for _, declaration := range declarations {
		field, ok := fields[declaration.ID]
		if !ok || !json.Valid(field.Value) || string(field.Value) == "null" {
			continue
		}
		switch field.Type {
		case protocol.FieldOption, protocol.FieldAccount, protocol.FieldOptions, protocol.FieldAccounts:
			many := field.Type == protocol.FieldOptions || field.Type == protocol.FieldAccounts
			options := make([]protocol.Option, 1)
			if many {
				if json.Unmarshal(field.Value, &options) != nil {
					continue
				}
			} else if json.Unmarshal(field.Value, &options[0]) != nil {
				continue
			}
			valid := true
			for i := range options {
				if options[i].ID == "" {
					valid = false
					break
				}
				if options[i].Label == "" {
					options[i].Label = options[i].ID
				}
			}
			if !valid {
				continue
			}
			if many {
				field = protocol.Value(field.Type, options)
			} else {
				field = protocol.Value(field.Type, options[0])
			}
		case protocol.FieldBoolean:
			var value bool
			if json.Unmarshal(field.Value, &value) != nil {
				continue
			}
		case protocol.FieldNumber:
			if field.Value[0] != '-' && (field.Value[0] < '0' || field.Value[0] > '9') {
				continue
			}
		case protocol.FieldDateTime:
			var value time.Time
			if json.Unmarshal(field.Value, &value) != nil {
				continue
			}
		default:
			if !protocol.ValidFieldValue(field) {
				continue
			}
		}
		result = append(result, common.WorkbenchTaskTopField{TaskTopField: declaration, Value: field})
	}
	return result
}
