package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alswl/apilot/connectors/protocol"
)

func TestGitHubMutationConfirmsCreateCloseAndReopen(t *testing.T) {
	connector := New(t.TempDir())
	connector.loadCredential = func(string, string) ([]byte, error) { return json.Marshal(storedCredential{Access: "test"}) }
	state, reason, title := "open", "", ""
	writes, reads := 0, 0
	const updated = "2026-11-23T12:00:00Z"
	issue := func() string {
		return fmt.Sprintf(`{"id":42,"number":3,"title":%q,"body":"body","state":%q,"state_reason":%q,"updated_at":%q}`, title, state, reason, updated)
	}
	connector.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != apiHost {
			t.Fatalf("escaped GitHub API origin: %s", req.URL)
		}
		switch {
		case req.Method == "GET" && req.URL.Path == "/repositories/7":
			return testAPIResponse(req, 200, `{"id":7,"has_issues":true,"permissions":{"push":true}}`, nil), nil
		case req.Method == "GET" && req.URL.Path == "/repositories/7/issues/3":
			reads++
			return testAPIResponse(req, 200, issue(), nil), nil
		case req.Method == "POST" && req.URL.Path == "/repositories/7/issues":
			writes++
			var body map[string]string
			if json.NewDecoder(req.Body).Decode(&body) != nil || body["title"] != "Created" {
				t.Fatal("wrong creation payload")
			}
			title = body["title"]
			return testAPIResponse(req, 201, issue(), nil), nil
		case req.Method == "PATCH" && req.URL.Path == "/repositories/7/issues/3":
			writes++
			var body map[string]string
			if json.NewDecoder(req.Body).Decode(&body) != nil {
				t.Fatal("wrong mutation payload")
			}
			if next := body["state"]; next != "" {
				state, reason = next, body["state_reason"]
			}
			if next := body["title"]; next != "" {
				title = next
			}
			return testAPIResponse(req, 200, issue(), nil), nil
		default:
			t.Fatalf("unexpected GitHub request %s %s", req.Method, req.URL)
		}
		return nil, errors.New("unexpected request")
	})
	ctx := context.Background()
	scope := protocol.ScopeContext{ScopeRef: "7", AccountRef: "42", EndpointRef: endpoint}
	create := protocol.MutationParams{ScopeContext: scope, OperationID: "create", Create: map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Created")}}
	var evidence protocol.MutationEvidence
	result, problem := connector.mutate(ctx, create, false, func(method string, input any) error {
		if method != "operation.evidence" {
			t.Fatal("wrong evidence notification")
		}
		evidence = input.(protocol.MutationEvidence)
		return nil
	})
	if problem != nil || result.(protocol.MutationResult).Status != "confirmed" || evidence.SourceID != "42" || writes != 1 || reads != 1 {
		t.Fatalf("create/readback: %+v %v", result, problem)
	}
	p := protocol.MutationParams{ScopeContext: scope, OperationID: "existing", SourceID: evidence.SourceID, Number: evidence.Number, ExpectedVersion: updated}
	p.Action = "close"
	result, problem = connector.mutate(ctx, p, false, nil)
	if problem != nil || result.(protocol.MutationResult).Record.State != "done" || writes != 2 || reads != 3 || reason != "completed" {
		t.Fatalf("close/readback: %+v %v", result, problem)
	}
	p.ExpectedVersion = "2026-11-22T12:00:00Z"
	result, problem = connector.mutate(ctx, p, false, nil)
	if problem != nil || result.(protocol.MutationResult).Status != "conflict" ||
		string(result.(protocol.MutationResult).Current["state"].Value) != `"closed"` || writes != 2 {
		t.Fatalf("stale action: %+v %v", result, problem)
	}
	p.ExpectedVersion = updated
	p.Action = "reopen"
	result, problem = connector.mutate(ctx, p, false, nil)
	if problem != nil || result.(protocol.MutationResult).Record.State != "open" || writes != 3 || reads != 6 {
		t.Fatalf("reopen/readback: %+v %v", result, problem)
	}
	p.Action = ""
	p.Patch = map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Changed")}
	p.Baseline = map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Older")}
	result, problem = connector.mutate(ctx, p, false, nil)
	if problem != nil || result.(protocol.MutationResult).Status != "conflict" || writes != 3 {
		t.Fatalf("conflict still wrote: %+v %v", result, problem)
	}
	p.Baseline["title"] = protocol.Value(protocol.FieldText, "Created")
	result, problem = connector.mutate(ctx, p, false, nil)
	if problem != nil || result.(protocol.MutationResult).Record.Title != "Changed" || writes != 4 {
		t.Fatalf("patch: %+v %v", result, problem)
	}
}

func TestGitHubLostCreationResponseCannotRedispatch(t *testing.T) {
	connector := New(t.TempDir())
	connector.loadCredential = func(string, string) ([]byte, error) { return json.Marshal(storedCredential{Access: "test"}) }
	writes := 0
	connector.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/issues") {
			writes++
			return nil, errors.New("lost response")
		}
		return testAPIResponse(req, 200, `{"id":7,"has_issues":true,"permissions":{"push":true}}`, nil), nil
	})
	params := protocol.MutationParams{ScopeContext: protocol.ScopeContext{ScopeRef: "7", AccountRef: "42", EndpointRef: endpoint},
		OperationID: "create", Create: map[string]protocol.FieldValue{"title": protocol.Value(protocol.FieldText, "Created")}}
	for _, reconcile := range []bool{false, true, true} {
		out, problem := connector.mutate(context.Background(), params, reconcile, nil)
		if problem != nil || out.(protocol.MutationResult).Status != "pending_confirmation" {
			t.Fatalf("unknown create: %+v %v", out, problem)
		}
	}
	if writes != 1 {
		t.Fatalf("original operation dispatched %d times", writes)
	}
}

func TestGitHubAuthoritativeAssigneeLookupAndConfirmation(t *testing.T) {
	client := newAPI("test", transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/users/octocat" {
			t.Fatalf("unexpected lookup %s", req.URL.Path)
		}
		return testAPIResponse(req, 200, `{"id":42,"login":"octocat"}`, nil), nil
	}))
	typed := protocol.Value(protocol.FieldAccounts, []protocol.Option{{ID: "octocat", Label: "octocat"}})
	values, problem := client.githubMutationBody(context.Background(), map[string]protocol.FieldValue{"assignees": typed})
	if problem != nil || len(values["assignees"].([]string)) != 1 || values["assignees"].([]string)[0] != "octocat" {
		t.Fatalf("typed account edit: %+v %v", values, problem)
	}
	current := protocol.Value(protocol.FieldAccounts, []protocol.Option{{ID: "42", Label: "Octocat"}})
	if !githubFieldMatches(current, typed) || githubFieldMatches(current, protocol.Value(protocol.FieldAccounts, []protocol.Option{{ID: "43", Label: "Octocat"}})) {
		t.Fatal("confirmation lost the account identity")
	}
}

func TestGitHubEditMetadataListsRepositoryLabels(t *testing.T) {
	c := New(t.TempDir())
	c.loadCredential = func(string, string) ([]byte, error) { return json.Marshal(storedCredential{Access: "test"}) }
	c.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repositories/7":
			return testAPIResponse(req, 200, `{"id":7,"full_name":"owner/selected","has_issues":true,"permissions":{"push":true}}`, nil), nil
		case "/repositories/7/issues/3":
			return testAPIResponse(req, 200, `{"id":42,"number":3,"state":"open","labels":[{"name":"assigned"}]}`, nil), nil
		case "/repos/owner/selected/labels":
			switch req.URL.Query().Get("page") {
			case "1":
				return testAPIResponse(req, 200, `[{"name":"assigned"},{"name":"archived","archived_at":"2026-11-22T12:00:00Z"}]`,
					http.Header{"Link": {`<https://api.github.com/repositories/7/labels?page=2&per_page=100>; rel="next"`}}), nil
			case "2":
				return testAPIResponse(req, 200, `[{"name":"available, too"}]`, nil), nil
			}
		}
		t.Fatalf("unexpected label request %s", req.URL)
		return nil, errors.New("unexpected request")
	})
	result, problem := c.editMetadata(context.Background(), protocol.DetailParams{
		ScopeContext: protocol.ScopeContext{ScopeRef: "7", AccountRef: "42", EndpointRef: endpoint}, SourceID: "42", Number: "3",
	})
	if problem != nil {
		t.Fatal(problem)
	}
	for _, field := range result.(protocol.ConnectorEditMetadataResult).Fields {
		if field.ID != "labels" {
			continue
		}
		if len(field.Options) != 2 || field.Options[0].ID != "assigned" || field.Options[1].ID != "available, too" {
			t.Fatalf("repository labels: %+v", field.Options)
		}
		return
	}
	t.Fatal("no editable labels")
}

func TestGitHubEditMetadataPreservesActionsWhenLabelsUnavailable(t *testing.T) {
	c := New(t.TempDir())
	c.loadCredential = func(string, string) ([]byte, error) { return json.Marshal(storedCredential{Access: "test"}) }
	c.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repositories/7":
			return testAPIResponse(req, 200, `{"id":7,"full_name":"owner/selected","has_issues":true,"permissions":{"push":true}}`, nil), nil
		case "/repositories/7/issues/3":
			return testAPIResponse(req, 200, `{"id":42,"number":3,"state":"open","updated_at":"2026-11-23T12:00:00Z"}`, nil), nil
		case "/repos/owner/selected/labels":
			return testAPIResponse(req, 503, `{}`, nil), nil
		}
		t.Fatalf("unexpected request %s", req.URL)
		return nil, errors.New("unexpected request")
	})
	result, problem := c.editMetadata(context.Background(), protocol.DetailParams{
		ScopeContext: protocol.ScopeContext{ScopeRef: "7", AccountRef: "42", EndpointRef: endpoint}, SourceID: "42", Number: "3",
	})
	if problem != nil {
		t.Fatal(problem)
	}
	meta := result.(protocol.ConnectorEditMetadataResult)
	if len(meta.Actions) != 2 || meta.Actions[0].ID != "close" || meta.Actions[1].ID != "close_not_planned" ||
		meta.Observation == nil || string(meta.Observation.Fields["state"].Value) != `"open"` {
		t.Fatalf("metadata after label failure: %+v", meta)
	}
	for _, field := range meta.Fields {
		if field.ID == "labels" {
			t.Fatal("unavailable labels still editable")
		}
	}
	if len(meta.Fields) == 0 {
		t.Fatal("unrelated edit fields disappeared")
	}
}
