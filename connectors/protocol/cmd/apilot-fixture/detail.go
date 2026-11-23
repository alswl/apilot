package main

import (
	"github.com/alswl/apilot/connectors/protocol"
)

func fixtureCatalog() protocol.FieldCatalog {
	definitions := []protocol.FieldDefinition{
		{ID: "title", Label: "Title", Type: protocol.FieldText, Writable: true},
		{ID: "description", Label: "Description", Type: protocol.FieldRichText, Writable: true},
		{ID: "assignees", Label: "Assignees", Type: protocol.FieldAccounts, Writable: true, Options: []protocol.Option{{ID: "alice", Label: "Alice"}, {ID: "bob", Label: "Bob"}}},
		{ID: "priority", Label: "Priority", Type: protocol.FieldNumber, Writable: true},
		{ID: "due_date", Label: "Due date", Type: protocol.FieldDate, Writable: true},
		{ID: "promised", Label: "Promised date", Type: protocol.FieldDate, Writable: true},
		{ID: "labels", Label: "Source labels", Type: protocol.FieldLabels, Writable: true},
		{ID: "status", Label: "Status", Type: protocol.FieldText},
	}
	result := protocol.FieldCatalog{Fields: definitions, Defaults: map[string]protocol.MappingRule{}}
	for _, field := range definitions {
		if field.ID != "promised" && field.ID != "status" {
			result.Defaults[field.ID] = protocol.MappingRule{Field: field.ID, Write: field.Writable}
		}
	}
	result.Defaults["state"] = protocol.MappingRule{Field: "status", Values: map[string]string{"todo": "open", "working": "in_progress", "closed": "done"}}
	return result
}

func (c fixtureConfig) detail(p protocol.DetailParams) (any, *protocol.RPCError) {
	if err := c.checkScope(p.ScopeContext); err != nil {
		return nil, err
	}
	if p.SourceID != "1" && p.SourceID != "2" {
		return nil, protocol.Problem(protocol.AccessDenied, "object", "Fixture task unavailable")
	}
	if c.DetailError != "" {
		kind := protocol.Temporary
		if c.DetailError == "denied" {
			kind = protocol.AccessDenied
		}
		return nil, protocol.Problem(kind, "object", "Fixture detail unavailable")
	}
	record := fixtureCurrent(p.ScopeRef, p.SourceID)
	if c.RemovedField != "" {
		delete(record.Fields, c.RemovedField)
	}
	return fixtureDetailPage(p, record)
}

func fixtureDetailPage(p protocol.DetailParams, record protocol.SourceRecord) (any, *protocol.RPCError) {
	page := protocol.DetailPage{Version: record.SourceVersion}
	switch p.Segment {
	case "fields":
		page.Fields = record.Fields
	case "description":
		page.Items = []protocol.DetailItem{{ID: "description", Label: "Description", Text: &protocol.RichText{Format: "markdown", Content: record.Fields["description"].Value}}}
	case "comments", "attachments", "links", "history":
		id := p.Segment + "-1"
		if p.Page == "second" {
			id = p.Segment + "-2"
		} else if p.Page == "" {
			page.NextPage = "second"
		} else {
			return nil, protocol.Problem(protocol.InvalidConfig, "object", "Invalid details page")
		}
		item := protocol.DetailItem{ID: id, Label: id}
		if p.Segment == "attachments" || p.Segment == "links" {
			item.URL = "https://fixture.example.test/" + id
		}
		if p.Segment == "comments" || p.Segment == "history" {
			item.Text = &protocol.RichText{Format: "markdown", Content: protocol.Value(protocol.FieldText, "Fixture "+id).Value}
		}
		page.Items = []protocol.DetailItem{item}
	default:
		return nil, protocol.Problem(protocol.InvalidConfig, "object", "Unknown detail segment")
	}
	return page, nil
}
