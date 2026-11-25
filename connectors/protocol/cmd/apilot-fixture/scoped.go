package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

var fixtureScopeMode string
var fixtureReleaseVersion = "0.1.0"

const scopedAccount = "synthetic-account"
const scopedEndpoint = "synthetic.local"

func scopedFixtureDescription(description *protocol.Description) {
	description.ScopeConfig.Schema = json.RawMessage(`{"type":"object","required":["target"],"properties":{"target":{"type":"string"}},"additionalProperties":false}`)
	description.ScopeConfig.Fields = map[string]protocol.ConfigField{"target": {Title: "Target", Control: "select", DynamicOptions: true}}
	description.ScopeConfig.Types = []string{protocol.ScopeProject, protocol.ScopeSpace}
	if fixtureScopeMode == "multi-endpoint2" {
		description.EndpointConfig.Version = 2
	}
	if fixtureScopeMode == "schema2" {
		description.ScopeConfig.Version = 2
	}
	if scopedMultiFixture() {
		description.EndpointConfig.Schema = json.RawMessage(`{"type":"object","required":["cluster","region"],"properties":{"cluster":{"type":"string","enum":["demo"]},"region":{"type":"string"}},"additionalProperties":false}`)
		description.EndpointConfig.Fields = map[string]protocol.ConfigField{
			"region": {Title: "Region", Control: "select", DynamicOptions: true, DependsOn: []string{"cluster"}},
		}
		description.ScopeConfig.Schema = json.RawMessage(`{"type":"object","required":["board","target"],"properties":{"board":{"type":"string","enum":["delivery"]},"target":{"type":"string"}},"additionalProperties":false}`)
		description.ScopeConfig.Fields["board"] = protocol.ConfigField{Title: "Board", DependsOn: []string{"endpoint.region"}}
		description.ScopeConfig.Fields["target"] = protocol.ConfigField{Title: "Target", Control: "select", DynamicOptions: true, DependsOn: []string{"board"}}
	}
	if fixtureScopeMode == "multi-endpoint2" {
		description.ScopeConfig.Schema = json.RawMessage(`{"type":"object","required":["board","target"],"properties":{"board":{"type":"string","enum":["delivery"]},"target":{"type":"string"},"metadata":{"type":"object"},"labels":{"type":"array","items":{"type":"string"}}},"additionalProperties":false}`)
	}
	if fixtureScopeMode == "project" {
		description.ScopeConfig.Types = []string{protocol.ScopeProject}
	}
}

func scopedFixture(ctx context.Context, req protocol.Request, _ protocol.Notify) (any, *protocol.RPCError) {
	config := fixtureConfig{Account: scopedAccount}
	badScope := protocol.Problem(protocol.InvalidConfig, "scope", "Unknown synthetic scope")
	badTarget := protocol.Problem(protocol.AccessDenied, "object", "Task is outside the synthetic scope")
	if req.Method == protocol.Login {
		path := filepath.Join(os.Getenv("APILOT_CONNECTOR_DATA_DIR"), "authorization.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, protocol.Problem(protocol.Temporary, "account", "Synthetic authorization log unavailable")
		}
		_, writeErr := fmt.Fprintln(f, req.Method)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return nil, protocol.Problem(protocol.Temporary, "account", "Synthetic authorization log unavailable")
		}
		var p protocol.LoginParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		if p.AccountRef != "" && p.AccountRef != scopedAccount || p.EndpointRef != "" && p.EndpointRef != scopedEndpoint {
			return nil, protocol.Problem(protocol.AccessDenied, "account", "Original synthetic account is required")
		}
		return protocol.Account{AccountRef: scopedAccount, EndpointRef: scopedEndpoint, DisplayName: "Synthetic Account"}, nil
	}
	if req.Method == protocol.Options {
		var p protocol.OptionsParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		if p.AccountRef != scopedAccount || p.EndpointRef != scopedEndpoint {
			return nil, badScope
		}
		if scopedMultiFixture() {
			var endpoint, scope map[string]string
			_ = json.Unmarshal(p.EndpointConfig, &endpoint)
			_ = json.Unmarshal(p.ScopeConfig, &scope)
			if p.ConfigLevel == "endpoint" && p.Field == "region" && endpoint["cluster"] == "demo" && p.Page == "" {
				return protocol.ConfigOptions{Items: []protocol.ConfigOption{{Value: "east", Label: "East"}}}, nil
			}
			if p.ConfigLevel != "scope" || endpoint["cluster"] != "demo" || endpoint["region"] != "east" || scope["board"] != "delivery" {
				return nil, badScope
			}
		}
		if p.Field != "target" {
			return nil, badScope
		}
		var options []protocol.ConfigOption
		switch p.ScopeType {
		case protocol.ScopeProject:
			options = []protocol.ConfigOption{{Value: "alpha", Label: "Alpha"}, {Value: "beta", Label: "Beta"}}
		case protocol.ScopeSpace:
			if fixtureScopeMode == "project" {
				return nil, badScope
			}
			options = []protocol.ConfigOption{{Value: "alpha", Label: "Alpha"}}
		default:
			return nil, badScope
		}
		token := base64.RawStdEncoding.EncodeToString([]byte(p.AccountRef + "\x00" + p.EndpointRef + "\x00" + p.Field + "\x00" + p.ScopeType + "\x00" + p.Search))
		result := protocol.ConfigOptions{Items: []protocol.ConfigOption{}}
		for _, option := range options {
			if strings.Contains(strings.ToLower(option.Label), strings.ToLower(p.Search)) {
				result.Items = append(result.Items, option)
			}
		}
		switch {
		case p.Page == "":
			if len(result.Items) > 1 {
				result.Items, result.NextPage = result.Items[:1], token
			}
		case p.Page == token && len(result.Items) > 1:
			result.Items = result.Items[1:]
		default:
			return nil, badScope
		}
		return result, nil
	}
	var scope protocol.ScopeContext
	if err := protocol.DecodeParams(req, &scope); err != nil {
		return nil, err
	}
	if scope.AccountRef != scopedAccount || scope.EndpointRef != scopedEndpoint {
		return nil, protocol.Problem(protocol.AuthRequired, "account", "Synthetic account unavailable")
	}
	var selection struct {
		Target   string                     `json:"target"`
		Board    string                     `json:"board"`
		Metadata map[string]json.RawMessage `json:"metadata"`
	}
	scopeVersion := 1
	if fixtureScopeMode == "schema2" {
		scopeVersion = 2
	}
	if json.Unmarshal(scope.ScopeConfig, &selection) != nil || scope.ScopeSchemaVersion != scopeVersion || (scope.EndpointSchemaVersion != 1 && (fixtureScopeMode != "multi-endpoint2" || scope.EndpointSchemaVersion != 2)) {
		return nil, badScope
	}
	if scopedMultiFixture() {
		var endpoint map[string]string
		if json.Unmarshal(scope.EndpointConfig, &endpoint) != nil || endpoint["cluster"] != "demo" || endpoint["region"] != "east" || selection.Board != "delivery" {
			return nil, badScope
		}
	}
	if counter := selection.Metadata["counter"]; counter != nil && string(counter) != "9007199254740993" {
		return nil, badScope
	}
	var projects []string
	switch {
	case scope.ScopeType == protocol.ScopeProject && (selection.Target == "alpha" || selection.Target == "beta"):
		projects = []string{selection.Target}
	case scope.ScopeType == protocol.ScopeSpace && selection.Target == "alpha" && fixtureScopeMode != "project":
		projects = []string{"alpha", "beta"}
	default:
		return nil, badScope
	}
	ref := scope.ScopeType + ":" + selection.Target
	if fixtureScopeMode == "renamed" {
		ref = "new:" + ref
	}
	if req.Method == protocol.Validate {
		var p protocol.ValidateParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		if p.Mode == "static" && scope.ScopeRef == "" || p.Mode == "access" && scope.ScopeRef == ref {
			return protocol.ValidatedScope{ScopeRef: ref, EndpointRef: scopedEndpoint, Label: map[string]string{"alpha": "Alpha", "beta": "Beta"}[selection.Target]}, nil
		}
		return nil, badScope
	}
	if scope.ScopeRef != ref {
		return nil, badScope
	}
	tracked := func(id string) bool {
		for _, project := range projects {
			if id == project {
				return true
			}
		}
		return false
	}
	switch req.Method {
	case protocol.Sync:
		var p protocol.SyncParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		if p.Page != "" || p.Mode != "full" && p.Mode != "delta" {
			return nil, badScope
		}
		result := protocol.SyncPage{Records: []protocol.SourceRecord{}, Coverage: p.Mode, NextCheckpoint: time.Now().UTC().Format(time.RFC3339Nano)}
		var previous time.Time
		if p.Mode == "delta" {
			var err error
			previous, err = time.Parse(time.RFC3339Nano, p.Checkpoint)
			if err != nil {
				return nil, protocol.Problem(protocol.InvalidCheckpoint, "scope", "Invalid synthetic checkpoint")
			}
		}
		for _, project := range projects {
			record := scopedRecord(project)
			if p.Mode == "full" || record.UpdatedAt.After(previous) {
				result.Records = append(result.Records, record)
			}
		}
		result.Usage = protocol.Usage{Requests: 1, Pages: 1, Received: len(result.Records)}
		return result, nil
	case protocol.Resolve:
		var p protocol.ResolveParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		result := protocol.ResolveResult{Items: []protocol.Resolution{}, Usage: protocol.Usage{Requests: 1}}
		for _, id := range p.SourceIDs {
			item := protocol.Resolution{SourceID: id, Status: "out_of_scope"}
			if tracked(id) {
				record := scopedRecord(id)
				item.Status, item.Record = "present", &record
			}
			result.Items = append(result.Items, item)
		}
		return result, nil
	case protocol.FieldsDescribe:
		return fixtureCatalog(), nil
	case protocol.TaskDetail, protocol.EditMetadata:
		var p protocol.DetailParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		if p.SourceID == "" && req.Method == protocol.EditMetadata {
			return protocol.EditMetadataResult{Fields: []protocol.FieldDefinition{}, Actions: []protocol.StateAction{}}, nil
		}
		if !tracked(p.SourceID) || p.Number != "" && p.Number != p.SourceID+"-1" {
			return nil, badTarget
		}
		record := scopedRecord(p.SourceID)
		if req.Method == protocol.TaskDetail {
			return fixtureDetailPage(p, record)
		}
		return protocol.ConnectorEditMetadataResult{EditMetadataResult: fixtureEditMetadata(record), Observation: &protocol.DetailPage{Version: record.SourceVersion, Fields: record.Fields}}, nil
	case protocol.Mutate, protocol.Reconcile:
		var p protocol.MutationParams
		if err := protocol.DecodeParams(req, &p); err != nil {
			return nil, err
		}
		project := p.SourceID
		if !tracked(project) || p.Number != "" && p.Number != project+"-1" {
			return nil, badTarget
		}
		p.ScopeRef, p.SourceID, p.EndpointRef = project, "1", fixtureEndpoint
		result, problem := config.mutation(ctx, p, req.Method == protocol.Reconcile)
		if problem != nil {
			return nil, problem
		}
		value := result.(protocol.MutationResult)
		if value.Record != nil {
			value.Record.SourceID = project
		}
		return value, nil
	}
	return nil, protocol.Problem(protocol.Unsupported, "", "Unknown synthetic method")
}

func scopedRecord(project string) protocol.SourceRecord {
	record := fixtureCurrent(project, "1")
	record.SourceID = project
	return record
}

func scopedMultiFixture() bool {
	return fixtureScopeMode == "multi" || fixtureScopeMode == "multi-endpoint2"
}
