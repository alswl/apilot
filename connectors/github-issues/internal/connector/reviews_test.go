package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/alswl/apilot/connectors/protocol"
)

func TestGitHubReviewRequestsUseCurrentAccountAndRepository(t *testing.T) {
	connector := New(t.TempDir())
	connector.loadCredential = func(string, string) ([]byte, error) { return json.Marshal(storedCredential{Access: "test"}) }
	var paths []string
	account, permission, repository, status := 42, "read", 7, http.StatusOK
	directReviewers := `[{"id":42}]`
	connector.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.Method != http.MethodGet || req.URL.Host != apiHost || req.Header.Get("Authorization") != "Bearer test" {
			t.Fatalf("unexpected review request: %s %s", req.Method, req.URL)
		}
		reply := func(body string) *http.Response { return testAPIResponse(req, 200, body, nil) }
		switch req.URL.Path {
		case "/user":
			return reply(fmt.Sprintf(`{"id":%d,"login":"reviewer"}`, account)), nil
		case "/user/installations":
			return reply(fmt.Sprintf(`{"installations":[{"id":1,"app_slug":"apilot-app","permissions":{"pull_requests":%q}}]}`, permission)), nil
		case "/user/installations/1/repositories":
			return reply(fmt.Sprintf(`{"repositories":[{"id":%d,"full_name":"owner/private","private":true}]}`, repository)), nil
		case "/repositories/7/pulls":
			if status != http.StatusOK {
				return testAPIResponse(req, status, `{}`, http.Header{"Retry-After": {"120"}}), nil
			}
			if req.URL.Query().Get("page") == "1" {
				header := http.Header{"Link": {`<https://api.github.com/repos/owner/private/pulls?per_page=100&page=2>; rel="next"`}}
				return testAPIResponse(req, 200, `[
					{"number":1,"title":"Others","state":"open","user":{"id":2},"requested_reviewers":[{"id":9}],"base":{"repo":{"id":7}}},
					{"number":2,"title":"Own","state":"open","user":{"id":42},"requested_reviewers":[{"id":42}],"base":{"repo":{"id":7}}},
					{"number":3,"title":"Team","state":"open","user":{"id":2},"requested_teams":[{"id":42}],"base":{"repo":{"id":7}}},
					{"number":4,"title":"Closed","state":"closed","user":{"id":2},"requested_reviewers":[{"id":42}],"base":{"repo":{"id":7}}}
				]`, header), nil
			}
			return reply(fmt.Sprintf(`[{"number":5,"title":"Requested","state":"open","user":{"id":2},"requested_reviewers":%s,"base":{"repo":{"id":7}}}]`, directReviewers)), nil
		default:
			t.Fatalf("unexpected endpoint %s", req.URL.Path)
			return nil, nil
		}
	})
	params, _ := json.Marshal(protocol.ScopeContext{AccountRef: "42", EndpointRef: endpoint, ScopeRef: "7"})
	read := func() (any, *protocol.RPCError) {
		return connector.Handle(context.Background(), protocol.Request{Method: protocol.GitHubReviewRequests, Params: params}, nil)
	}
	result, err := read()
	if err != nil || !slices.Equal(result.(protocol.GitHubReviewRequestsResult).Items, []protocol.GitHubReviewRequest{{Repository: "owner/private", Number: 5, Title: "Requested"}}) {
		t.Fatalf("review requests: %+v, %v", result, err)
	}
	directReviewers = "[]"
	result, err = read()
	if err != nil || len(result.(protocol.GitHubReviewRequestsResult).Items) != 0 {
		t.Fatalf("removed request: %+v, %v", result, err)
	}

	for _, tc := range []struct {
		name                               string
		account, repository, status, calls int
		permission                         string
		kind                               protocol.ErrorKind
	}{
		{"identity changed", 8, 7, 200, 1, "read", protocol.AuthRequired},
		{"permission missing", 42, 7, 200, 2, "none", protocol.AccessDenied},
		{"installation access revoked", 42, 8, 200, 3, "read", protocol.AccessDenied},
		{"private repo unavailable", 42, 7, 404, 4, "read", protocol.AccessDenied},
		{"rate limited", 42, 7, 429, 4, "read", protocol.RateLimited},
		{"server failure", 42, 7, 502, 4, "read", protocol.Temporary},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths = nil
			account, repository, status, permission = tc.account, tc.repository, tc.status, tc.permission
			result, err := read()
			if result != nil || err == nil || err.Data.Kind != tc.kind || len(paths) != tc.calls {
				t.Fatalf("response: %+v, %+v, requested %v", result, err, paths)
			}
			if tc.kind == protocol.RateLimited && err.Data.RetryAt == nil {
				t.Fatal("rate limit omitted backoff")
			}
		})
	}
}
