package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

func (c *apiClient) writableRepository(ctx context.Context, scope string) (bool, string, *protocol.RPCError) {
	var repo struct {
		ID          int64  `json:"id"`
		FullName    string `json:"full_name"`
		HasIssues   bool   `json:"has_issues"`
		Permissions struct {
			Push     bool `json:"push"`
			Maintain bool `json:"maintain"`
			Admin    bool `json:"admin"`
		} `json:"permissions"`
	}
	if _, error := c.get(ctx, "/repositories/"+scope, nil, &repo); error != nil {
		return false, "", error
	}
	return repo.HasIssues && strconv.FormatInt(repo.ID, 10) == scope &&
		(repo.Permissions.Push || repo.Permissions.Maintain || repo.Permissions.Admin), repo.FullName, nil
}

func (c *apiClient) readMutationIssue(ctx context.Context, scope, id, number string) (githubIssue, *protocol.RPCError) {
	var issue githubIssue
	if !positiveID(scope) || !positiveID(id) || !positiveID(number) {
		return issue, protocol.Problem(protocol.InvalidConfig, "object", "Invalid issue reference")
	}
	if _, problem := c.get(ctx, "/repositories/"+scope+"/issues/"+number, nil, &issue); problem != nil {
		return issue, problem
	}
	if strconv.FormatInt(issue.ID, 10) != id ||
		strconv.FormatInt(issue.Number, 10) != number ||
		(len(issue.PullRequest) > 0 && string(issue.PullRequest) != "null") {
		return githubIssue{}, protocol.Problem(protocol.AccessDenied, "object", "GitHub issue identity changed")
	}
	return issue, nil
}

func githubEditFields() []protocol.FieldDefinition {
	fields := []protocol.FieldDefinition{}
	for _, field := range githubCatalog().Fields {
		if field.Writable {
			fields = append(fields, field)
		}
	}
	return fields
}

func (c *Connector) editMetadata(ctx context.Context, p protocol.DetailParams) (any, *protocol.RPCError) {
	out := protocol.EditMetadataResult{Fields: []protocol.FieldDefinition{}, Actions: []protocol.StateAction{}}
	if !positiveID(p.ScopeRef) {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Invalid GitHub repository")
	}
	token, problem := c.token(ctx, p.AccountRef, p.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	writable, repo, problem := client.writableRepository(ctx, p.ScopeRef)
	if problem != nil {
		return nil, problem
	}
	if !writable {
		return out, nil
	}
	if p.SourceID == "" {
		out.Create = true
	} else {
		issue, problem := client.readMutationIssue(ctx, p.ScopeRef, p.SourceID, p.Number)
		if problem != nil {
			return nil, problem
		}
		out.Actions = githubActions(issue)
	}
	labels, problem := client.repositoryLabelOptions(ctx, repo, p.ScopeRef)
	if problem != nil {
		return nil, problem
	}
	out.Fields = githubEditFields()
	for i := range out.Fields {
		if out.Fields[i].ID == "labels" {
			out.Fields[i].Options = labels
		}
	}
	return out, nil
}

func (c *apiClient) repositoryLabelOptions(ctx context.Context, repo, scope string) ([]protocol.Option, *protocol.RPCError) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || owner == "." || owner == ".." || name == "." || name == ".." {
		return nil, protocol.Problem(protocol.Temporary, "scope", "Invalid GitHub repository name")
	}
	path := "/repos/" + repo + "/labels"
	var options []protocol.Option
	seen := map[string]bool{}
	problem := c.pages(ctx, path, func(query url.Values) (http.Header, *protocol.RPCError) {
		var page []struct {
			Name       string     `json:"name"`
			ArchivedAt *time.Time `json:"archived_at"`
		}
		header, problem := c.get(ctx, path, query, &page)
		if problem != nil {
			return nil, problem
		}
		for _, label := range page {
			if label.Name != "" && label.ArchivedAt == nil && !seen[label.Name] {
				options = append(options, protocol.Option{ID: label.Name, Label: label.Name})
				seen[label.Name] = true
			}
		}
		return header, nil
	}, "/repositories/"+scope+"/labels")
	return options, problem
}

func githubMutationFields(issue githubIssue) map[string]protocol.FieldValue {
	record, _ := issueRecord(issue)
	if record.Fields == nil {
		record.Fields = map[string]protocol.FieldValue{}
	}
	record.Fields["state"] = protocol.Value(protocol.FieldText, issue.State)
	record.Fields["title"] = protocol.Value(protocol.FieldText, issue.Title)
	record.Fields["body"] = protocol.Value(protocol.FieldRichText, issue.Body)
	return record.Fields
}

func githubFieldMatches(a, b protocol.FieldValue) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case protocol.FieldAccounts:
		var av, bv []protocol.Option
		if json.Unmarshal(a.Value, &av) != nil || json.Unmarshal(b.Value, &bv) != nil {
			return false
		}
		if len(av) != len(bv) {
			return false
		}
		matched := make([]bool, len(av))
		for _, wanted := range bv {
			index := -1
			for i, current := range av {
				if matched[i] {
					continue
				}
				if wanted.ID == current.ID || (!positiveID(wanted.ID) && wanted.ID == wanted.Label && strings.EqualFold(wanted.Label, current.Label)) {
					index = i
					break
				}
			}
			if index < 0 {
				return false
			}
			matched[index] = true
		}
		return true
	case protocol.FieldLabels:
		var av, bv []string
		if json.Unmarshal(a.Value, &av) != nil || json.Unmarshal(b.Value, &bv) != nil {
			return false
		}
		slices.Sort(av)
		slices.Sort(bv)
		return slices.Equal(av, bv)
	case protocol.FieldRichText:
		var av, bv string
		return json.Unmarshal(a.Value, &av) == nil && json.Unmarshal(b.Value, &bv) == nil && av == bv
	default:
		return protocol.FieldEqual(a, b)
	}
}

func (c *apiClient) githubMutationBody(ctx context.Context, fields map[string]protocol.FieldValue) (map[string]any, *protocol.RPCError) {
	result := make(map[string]any)
	for key, value := range fields {
		if !protocol.ValidFieldValue(value) {
			return nil, protocol.Problem(protocol.InvalidConfig, "object", "Invalid GitHub field value")
		}
		switch key {
		case "title", "body":
			var text string
			if json.Unmarshal(value.Value, &text) != nil ||
				(key == "title" && (value.Type != protocol.FieldText || strings.TrimSpace(text) == "" || strings.ContainsAny(text, "\r\n\x00"))) ||
				(key == "body" && value.Type != protocol.FieldRichText) {
				return nil, protocol.Problem(protocol.InvalidConfig, "object", "Invalid GitHub text value")
			}
			result[key] = text
		case "labels":
			items := []string{}
			if value.Type != protocol.FieldLabels || json.Unmarshal(value.Value, &items) != nil {
				return nil, protocol.Problem(protocol.InvalidConfig, "object", "Invalid GitHub labels")
			}
			if items == nil {
				items = []string{}
			}
			result[key] = items
		case "assignees":
			var accounts []protocol.Option
			if value.Type != protocol.FieldAccounts || json.Unmarshal(value.Value, &accounts) != nil {
				return nil, protocol.Problem(protocol.InvalidConfig, "object", "Invalid GitHub assignees")
			}
			logins := []string{}
			for _, account := range accounts {
				var user struct {
					ID    int64  `json:"id"`
					Login string `json:"login"`
				}
				if account.Label == "" || (!positiveID(account.ID) && account.ID != account.Label) || strings.ContainsAny(account.Label, "/\\%?#") {
					return nil, protocol.Problem(protocol.InvalidConfig, "object", "Invalid GitHub assignee")
				}
				if _, problem := c.get(ctx, "/users/"+account.Label, nil, &user); problem != nil {
					return nil, problem
				}
				if user.ID <= 0 || !strings.EqualFold(user.Login, account.Label) || (positiveID(account.ID) && strconv.FormatInt(user.ID, 10) != account.ID) {
					return nil, protocol.Problem(protocol.InvalidConfig, "object", "GitHub assignee identity changed")
				}
				logins = append(logins, user.Login)
			}
			result[key] = logins
		default:
			return nil, protocol.Problem(protocol.InvalidConfig, "object", "GitHub field is read only")
		}
	}
	return result, nil
}

func (c *Connector) mutate(ctx context.Context, p protocol.MutationParams, reconcile bool, notify protocol.Notify) (any, *protocol.RPCError) {
	pending := protocol.MutationResult{Status: "pending_confirmation"}
	rejected := protocol.MutationResult{Status: "not_executed"}
	if !p.Valid(reconcile) || !positiveID(p.ScopeRef) {
		return rejected, nil
	}
	creating := len(p.Create) > 0
	if reconcile && p.SourceID == "" {
		return pending, nil
	}
	token, problem := c.token(ctx, p.AccountRef, p.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	if !reconcile {
		writable, _, problem := client.writableRepository(ctx, p.ScopeRef)
		if problem != nil {
			return nil, problem
		}
		if !writable {
			return rejected, nil
		}
	}
	var issue githubIssue
	if !creating || reconcile {
		issue, problem = client.readMutationIssue(ctx, p.ScopeRef, p.SourceID, p.Number)
		if problem != nil {
			return nil, problem
		}
	}
	if !reconcile {
		var body map[string]any
		if len(p.Patch) > 0 || creating {
			values := p.Patch
			if creating {
				values = p.Create
			}
			current := githubMutationFields(issue)
			if !creating {
				for key := range values {
					if !githubFieldMatches(current[key], p.Baseline[key]) {
						return protocol.MutationResult{Status: "conflict", Current: current}, nil
					}
				}
			}
			body, problem = client.githubMutationBody(ctx, values)
			if problem != nil {
				return rejected, nil
			}
			if creating && body["title"] == nil {
				return rejected, nil
			}
		} else if p.Action != "" {
			state, reason := githubTransition(p.Action)
			if state == "" {
				return rejected, nil
			}
			if issue.UpdatedAt.UTC().Format(time.RFC3339Nano) != p.ExpectedVersion {
				return protocol.MutationResult{Status: "conflict", Current: githubMutationFields(issue)}, nil
			}
			if state == issue.State {
				return rejected, nil
			}
			body = map[string]any{"state": state}
			if reason != "" {
				body["state_reason"] = reason
			}
		} else {
			return rejected, nil
		}
		method, path := http.MethodPatch, "/repositories/"+p.ScopeRef+"/issues/"+p.Number
		if creating {
			method, path = http.MethodPost, "/repositories/"+p.ScopeRef+"/issues"
		}
		issue, code := client.write(ctx, method, path, body)
		switch code {
		case 200, 201:
		case 400, 401, 403, 404, 422, 429:
			return rejected, nil
		default:
			return pending, nil
		}
		if creating {
			if issue.ID <= 0 || issue.Number <= 0 {
				return pending, nil
			}
			p.SourceID = strconv.FormatInt(issue.ID, 10)
			p.Number = strconv.FormatInt(issue.Number, 10)
			evidence := protocol.MutationEvidence{SourceID: p.SourceID, Number: p.Number}
			pending.Evidence = &evidence
			if notify != nil {
				if err := notify("operation.evidence", evidence); err != nil {
					return pending, nil
				}
			}
		}
		result, err := client.readMutationIssue(ctx, p.ScopeRef, p.SourceID, p.Number)
		if err != nil {
			return pending, nil
		}
		return githubConfirmed(p, result, pending), nil
	}
	return githubConfirmed(p, issue, pending), nil
}

func githubConfirmed(p protocol.MutationParams, issue githubIssue, pending protocol.MutationResult) protocol.MutationResult {
	current := githubMutationFields(issue)
	for key, value := range p.Patch {
		if !githubFieldMatches(current[key], value) {
			pending.Current = current
			return pending
		}
	}
	for key, value := range p.Create {
		if !githubFieldMatches(current[key], value) {
			pending.Current = current
			return pending
		}
	}
	if p.Action != "" {
		state, reason := githubTransition(p.Action)
		if issue.State != state || (reason != "" && issue.StateReason != reason) {
			return pending
		}
	}
	record, ok := issueRecord(issue)
	if !ok {
		return pending
	}
	record.Fields = current
	return protocol.MutationResult{Status: "confirmed", Record: &record, Evidence: pending.Evidence}
}

func (c *apiClient) write(ctx context.Context, method, path string, body any) (githubIssue, int) {
	var result githubIssue
	data, err := json.Marshal(body)
	if err != nil {
		return result, 0
	}
	target := url.URL{Scheme: "https", Host: apiHost, Path: path}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(data))
	if err != nil {
		return result, 0
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	resp, err := c.http.Do(req)
	if err != nil {
		return result, 0
	}
	defer resp.Body.Close()
	if (resp.StatusCode == 200 || resp.StatusCode == 201) && json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result) != nil {
		return result, 0
	}
	return result, resp.StatusCode
}
