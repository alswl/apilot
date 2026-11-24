package connector

import (
	"context"

	"github.com/alswl/apilot/connectors/protocol"
)

func githubCatalog() protocol.FieldCatalog {
	fields := []protocol.FieldDefinition{
		{ID: "title", Label: "Title", Type: protocol.FieldText, Writable: true},
		{ID: "body", Label: "Body", Type: protocol.FieldRichText, Writable: true},
		{ID: "assignees", Label: "Assignees", Type: protocol.FieldAccounts, Writable: true},
		{ID: "labels", Label: "Source labels", Type: protocol.FieldLabels, Writable: true},
		{ID: "state_reason", Label: "State reason", Type: protocol.FieldText},
	}
	return protocol.FieldCatalog{Fields: fields, Defaults: map[string]protocol.MappingRule{
		"title": {Field: "title", Write: true}, "description": {Field: "body", Write: true},
		"assignees": {Field: "assignees", Write: true}, "labels": {Field: "labels", Write: true},
	}}
}

func (c *Connector) detail(ctx context.Context, p protocol.DetailParams) (any, *protocol.RPCError) {
	if !positiveID(p.ScopeRef) || !positiveID(p.Number) || !positiveID(p.SourceID) || p.Page != "" {
		return nil, protocol.Problem(protocol.InvalidConfig, "object", "Invalid GitHub detail request")
	}
	token, problem := c.token(ctx, p.AccountRef, p.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	var issue githubIssue
	if _, problem := client.get(ctx, "/repositories/"+p.ScopeRef+"/issues/"+p.Number, nil, &issue); problem != nil {
		return nil, problem
	}
	record, ok := issueRecord(issue)
	if !ok || record.SourceID != p.SourceID || record.Number != p.Number {
		return nil, protocol.Problem(protocol.AccessDenied, "object", "GitHub issue could not be verified")
	}
	page := protocol.DetailPage{Version: record.SourceVersion}
	switch p.Segment {
	case "fields":
		page.Fields = githubMutationFields(issue)
		page.Fields["repository_id"] = protocol.Value(protocol.FieldText, p.ScopeRef)
	case "description":
		page.Items = []protocol.DetailItem{{ID: "body", Label: "Body", Text: &protocol.RichText{Format: "markdown", Content: protocol.Value(protocol.FieldText, issue.Body).Value}}}
	default:
		return nil, protocol.Problem(protocol.Unsupported, "object", "GitHub details support fields and description")
	}
	return page, nil
}
