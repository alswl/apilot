package connector

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

type githubIssue struct {
	Body        string          `json:"body"`
	ID          int64           `json:"id"`
	Number      int64           `json:"number"`
	Title       string          `json:"title"`
	HTMLURL     string          `json:"html_url"`
	State       string          `json:"state"`
	StateReason string          `json:"state_reason"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"assignees"`
}

type readCursor struct {
	Binding string    `json:"binding"`
	Window  time.Time `json:"window"`
	Page    int       `json:"page,omitempty"`
}

func scopeBinding(scope protocol.ScopeContext, fields []string) string {
	data, _ := json.Marshal(struct {
		Scope  protocol.ScopeContext
		Fields []string
	}{scope, fields})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func unpackCursor(raw string) (readCursor, bool) {
	var cursor readCursor
	if len(raw) > 512 {
		return cursor, false
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	ok := err == nil && json.Unmarshal(data, &cursor) == nil && !cursor.Window.IsZero() && cursor.Window.Before(time.Now().Add(time.Minute))
	return cursor, ok
}

func packCursor(cursor readCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}

func (c *Connector) sync(ctx context.Context, p protocol.SyncParams) (any, *protocol.RPCError) {
	if !positiveID(p.ScopeRef) || (p.Mode != "full" && p.Mode != "delta") {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Invalid GitHub scope or sync mode")
	}
	binding := scopeBinding(p.ScopeContext, p.Fields)
	query := url.Values{"state": {"all"}, "per_page": {"100"}, "sort": {"created"}, "direction": {"asc"}}
	if p.Mode == "delta" {
		checkpoint, ok := unpackCursor(p.Checkpoint)
		if !ok || checkpoint.Binding != binding || checkpoint.Page != 0 {
			return nil, protocol.Problem(protocol.InvalidCheckpoint, "scope", "GitHub checkpoint is invalid")
		}
		query.Set("since", checkpoint.Window.Add(-2*time.Minute).UTC().Format(time.RFC3339))
		query.Set("sort", "updated")
	}
	pageBinding := fmt.Sprintf("%x", sha256.Sum256([]byte(binding+"|"+p.Mode+"|"+query.Encode())))
	cursor := readCursor{Binding: pageBinding, Window: time.Now().UTC(), Page: 1}
	if p.Page != "" {
		next, ok := unpackCursor(p.Page)
		if !ok || next.Binding != pageBinding || next.Page < 2 || next.Page > 10000 {
			return nil, protocol.Problem(protocol.InvalidConfig, "scope", "GitHub page is invalid")
		}
		cursor = next
	}
	query.Set("page", strconv.Itoa(cursor.Page))
	token, problem := c.token(ctx, p.AccountRef, p.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	path := "/repositories/" + p.ScopeRef + "/issues"
	var issues []githubIssue
	client.usage.Pages++
	header, problem := client.get(ctx, path, query, &issues)
	if problem != nil {
		if problem.Data.Diagnostic == "404" {
			if verified := client.confirmRepository(ctx, p.ScopeRef); verified != nil {
				problem = verified
			}
		}
		problem.Data.Usage = &client.usage
		return nil, problem
	}
	result := protocol.SyncPage{Records: []protocol.SourceRecord{}}
	for _, issue := range issues {
		if len(issue.PullRequest) > 0 && string(issue.PullRequest) != "null" {
			continue
		}
		record, ok := issueRecord(issue)
		if !ok {
			problem := protocol.Problem(protocol.Temporary, "scope", "GitHub returned an issue without a stable identity")
			problem.Data.Usage = &client.usage
			return nil, problem
		}
		result.Records = append(result.Records, record)
		client.usage.Received++
	}
	next, problem := nextPage(header, path, query)
	if problem == nil && next != 0 && next != cursor.Page+1 {
		problem = protocol.Problem(protocol.Temporary, "scope", "GitHub pagination did not advance")
	}
	if problem != nil {
		problem.Data.Usage = &client.usage
		return nil, problem
	}
	if next == 0 {
		result.Coverage = p.Mode
		result.NextCheckpoint = packCursor(readCursor{Binding: binding, Window: cursor.Window})
	} else {
		cursor.Page = next
		result.NextPage = packCursor(cursor)
	}
	result.Usage = client.usage
	return result, nil
}

func (c *apiClient) confirmRepository(ctx context.Context, id string) *protocol.RPCError {
	repositories, problem := c.repositories(ctx)
	if problem != nil {
		return problem
	}
	for _, repo := range repositories {
		if strconv.FormatInt(repo.ID, 10) == id {
			return nil
		}
	}
	return protocol.Problem(protocol.AccessDenied, "scope", "The GitHub App can no longer access the repository")
}

func (c *Connector) resolve(ctx context.Context, p protocol.ResolveParams) (any, *protocol.RPCError) {
	if !positiveID(p.ScopeRef) || len(p.SourceIDs) > 100 {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Invalid GitHub resolution request")
	}
	token, problem := c.token(ctx, p.AccountRef, p.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	result := protocol.ResolveResult{Items: []protocol.Resolution{}}
	for _, id := range p.SourceIDs {
		number := p.KnownNumbers[id]
		item := protocol.Resolution{SourceID: id, Status: "unverified"}
		if positiveID(id) && positiveID(number) {
			path := "/repositories/" + p.ScopeRef + "/issues/" + number
			var issue githubIssue
			header, problem := client.get(ctx, path, nil, &issue)
			switch {
			case problem == nil:
				if record, ok := issueRecord(issue); ok && record.SourceID == id && record.Number == number && (len(issue.PullRequest) == 0 || string(issue.PullRequest) == "null") {
					item.Status, item.Record = "present", &record
					client.usage.Received++
				}
			case problem.Data.Diagnostic == "410":
				item.Status = "deleted"
			case problem.Data.Diagnostic == "301" || problem.Data.Diagnostic == "302" || problem.Data.Diagnostic == "307" || problem.Data.Diagnostic == "308":
				target, err := url.Parse(header.Get("Location"))
				if err == nil && target.Scheme == "https" && target.Host == apiHost && target.User == nil {
					var repo, num int64
					if n, _ := fmt.Sscanf(target.Path, "/repositories/%d/issues/%d", &repo, &num); n == 2 && target.Path == fmt.Sprintf("/repositories/%d/issues/%d", repo, num) && strconv.FormatInt(repo, 10) != p.ScopeRef {
						item.Status = "out_of_scope"
					}
				}
			case problem.Data.Diagnostic == "404":
				if verified := client.confirmRepository(ctx, p.ScopeRef); verified != nil {
					verified.Data.Usage = &client.usage
					return nil, verified
				}
			default:
				problem.Data.Usage = &client.usage
				return nil, problem
			}
		}
		result.Items = append(result.Items, item)
	}
	result.Usage = client.usage
	return result, nil
}

func issueRecord(issue githubIssue) (protocol.SourceRecord, bool) {
	if issue.ID <= 0 || issue.Number <= 0 || issue.Title == "" || issue.UpdatedAt.IsZero() {
		return protocol.SourceRecord{}, false
	}
	state := "unknown"
	switch {
	case issue.State == "open":
		state = "open"
	case issue.State == "closed" && issue.StateReason == "completed":
		state = "done"
	case issue.State == "closed" && (issue.StateReason == "not_planned" || issue.StateReason == "duplicate"):
		state = "cancelled"
	}
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		labels = append(labels, label.Name)
	}
	assignees := make([]protocol.Option, 0, len(issue.Assignees))
	for _, assignee := range issue.Assignees {
		if assignee.ID > 0 && assignee.Login != "" {
			assignees = append(assignees, protocol.Option{ID: strconv.FormatInt(assignee.ID, 10), Label: assignee.Login})
		}
	}
	labelJSON, _ := json.Marshal(labels)
	accountJSON, _ := json.Marshal(assignees)
	return protocol.SourceRecord{
		SourceID: strconv.FormatInt(issue.ID, 10), Number: strconv.FormatInt(issue.Number, 10),
		SourceVersion: issue.UpdatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: issue.UpdatedAt, URL: issue.HTMLURL,
		Title: issue.Title, State: state, StateID: issue.State, StateLabel: issue.State,
		Fields: map[string]protocol.FieldValue{
			"title":        protocol.Value(protocol.FieldText, issue.Title),
			"state_reason": {Type: protocol.FieldText, Value: json.RawMessage(strconv.Quote(issue.StateReason))},
			"labels":       {Type: protocol.FieldLabels, Value: labelJSON},
			"assignees":    {Type: protocol.FieldAccounts, Value: accountJSON},
		},
	}, true
}
