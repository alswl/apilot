package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

func fixtureTaskPath(scope, id string) string {
	return filepath.Join(os.Getenv("APILOT_CONNECTOR_DATA_DIR"), "task-"+scope+"-"+id+".json")
}

func fixtureCurrent(scope, id string) protocol.SourceRecord {
	if data, err := os.ReadFile(fixtureTaskPath(scope, id)); err == nil {
		var task protocol.SourceRecord
		if json.Unmarshal(data, &task) == nil && task.SourceID == id {
			return task
		}
	}
	return fixtureRecord(scope, id, nil)
}

func fixtureEditMetadata(record protocol.SourceRecord) protocol.EditMetadataResult {
	result := protocol.EditMetadataResult{Fields: []protocol.FieldDefinition{}, Actions: []protocol.StateAction{}}
	for _, field := range fixtureCatalog().Fields {
		if field.Writable {
			result.Fields = append(result.Fields, field)
		}
	}
	switch record.State {
	case "open", "in_progress":
		result.Actions = append(result.Actions, protocol.StateAction{ID: "resolve", Label: "Resolve", Required: []protocol.FieldDefinition{{ID: "resolution", Label: "Resolution", Type: protocol.FieldText}}})
	case "done":
		result.Actions = append(result.Actions, protocol.StateAction{ID: "reopen", Label: "Reopen"})
	}
	return result
}

func (c fixtureConfig) mutation(ctx context.Context, p protocol.MutationParams, reconcile bool) (any, *protocol.RPCError) {
	if err := c.checkScope(p.ScopeContext); err != nil {
		return nil, err
	}
	if !p.Valid(reconcile) || (p.SourceID != "1" && p.SourceID != "2") {
		return protocol.MutationResult{Status: "not_executed"}, nil
	}
	current := fixtureCurrent(p.ScopeRef, p.SourceID)
	matches := true
	for field, desired := range p.Patch {
		matches = matches && protocol.FieldEqual(current.Fields[field], desired)
	}
	if p.Action == "resolve" {
		matches = current.State == "done"
	}
	if p.Action == "reopen" {
		matches = current.State == "open"
	}
	if reconcile {
		if matches {
			return protocol.MutationResult{Status: "confirmed", Record: &current}, nil
		}
		return protocol.MutationResult{Status: "pending_confirmation"}, nil
	}
	if c.ReadOnlyTask == p.ScopeRef+"-"+p.SourceID {
		return protocol.MutationResult{Status: "not_executed"}, nil
	}
	if c.WriteResult == "conflict" || (p.Action != "" && p.ExpectedVersion != current.SourceVersion) {
		return protocol.MutationResult{Status: "conflict", Current: current.Fields}, nil
	}
	definitions := map[string]protocol.FieldDefinition{}
	for _, field := range fixtureEditMetadata(current).Fields {
		definitions[field.ID] = field
	}
	for field, desired := range p.Patch {
		native := definitions[field]
		if !native.Writable || native.Type != desired.Type || !protocol.ValidFieldValue(desired) || (field == "title" && string(desired.Value) == "null") {
			return protocol.MutationResult{Status: "not_executed"}, nil
		}
		if !protocol.FieldEqual(current.Fields[field], p.Baseline[field]) {
			return protocol.MutationResult{Status: "conflict", Current: current.Fields}, nil
		}
		current.Fields[field] = desired
	}
	if p.Action != "" {
		switch {
		case p.Action == "resolve" && current.State != "done":
			var resolution string
			if json.Unmarshal(p.ActionFields["resolution"].Value, &resolution) != nil || resolution == "" {
				return protocol.MutationResult{Status: "not_executed"}, nil
			}
			current.State, current.StateID, current.StateLabel = "done", "closed", "Closed"
		case p.Action == "reopen" && current.State == "done":
			current.State, current.StateID, current.StateLabel = "open", "todo", "To do"
		default:
			return protocol.MutationResult{Status: "not_executed"}, nil
		}
		current.Fields["status"] = protocol.Value(protocol.FieldText, current.StateID)
	}
	var title string
	if json.Unmarshal(current.Fields["title"].Value, &title) == nil && title != "" {
		current.Title = title
	}
	if ctx.Err() != nil {
		return nil, protocol.Problem(protocol.Temporary, "object", "Fixture write interrupted")
	}
	current.UpdatedAt = time.Now().UTC()
	current.SourceVersion = current.UpdatedAt.Format(time.RFC3339Nano)
	record, err := json.Marshal(current)
	if err != nil {
		return nil, protocol.Problem(protocol.Temporary, "object", "Fixture source unavailable")
	}
	target := fixtureTaskPath(p.ScopeRef, p.SourceID)
	file, err := os.CreateTemp(filepath.Dir(target), "fixture-write-*")
	if err != nil {
		return nil, protocol.Problem(protocol.Temporary, "object", "Fixture source unavailable")
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(record)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil || os.Rename(file.Name(), target) != nil {
		return nil, protocol.Problem(protocol.Temporary, "object", "Fixture write interrupted")
	}
	if c.WriteResult == "lost" {
		return nil, protocol.Problem(protocol.Temporary, "object", "Fixture write response lost")
	}
	readback := fixtureCurrent(p.ScopeRef, p.SourceID)
	return protocol.MutationResult{Status: "confirmed", Record: &readback}, nil
}
