package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestGitHubRepositoryPartsMatchesIssuePathValidation(t *testing.T) {
	parts, ok := GitHubRepositoryParts("owner/repo")
	if !ok || !slices.Equal(parts, []string{"owner", "repo"}) {
		t.Fatalf("valid repository parsed as %v, %t", parts, ok)
	}
	for _, repository := range []string{"", "owner", "/repo", "owner/", "owner//repo", "owner/../repo", "./repo", "owner/re po", "owner/re\tpo", "owner/repo?x", "owner/repo%2Fother", "owner/repo\\other"} {
		if parts, ok := GitHubRepositoryParts(repository); ok {
			t.Errorf("invalid repository %q parsed as %v", repository, parts)
		}
	}
}

func TestGitHubReadDistinguishesTimeout(t *testing.T) {
	client, err := NewWorkbenchGitHubClient("https://api.github.com", "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err = client.Identity(ctx)
	problem, ok := err.(*common.Error)
	if !ok || problem.Code != "SOURCE_UNAVAILABLE" || problem.Message != "GitHub request timed out" {
		t.Fatalf("unexpected timeout result: %v", err)
	}
}

func TestGitHubIssuesRejectsPaginationChangingFilters(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		query.Set("state", "open")
		query.Set("page", "2")
		w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, query.Encode()))
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Issues(context.Background(), common.WorkbenchConnection{Repository: "a/b"}, nil, 0)
	problem, ok := err.(*common.Error)
	if !ok || problem.Code != "SOURCE_UNAVAILABLE" || problem.Message != "GitHub pagination changed the issue query" {
		t.Fatalf("unexpected pagination result: %v", err)
	}
}

func TestGitHubIssuesCanonicalPaginationRequiresRepositoryID(t *testing.T) {
	for _, tc := range []struct {
		name      string
		linkID    int
		wantError bool
	}{{"same repository", 42, false}, {"different repository", 43, true}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("page") == "" {
					query := r.URL.Query()
					query.Set("page", "2")
					w.Header().Set("Link", fmt.Sprintf(`<https://%s/repositories/%d/issues?%s>; rel="next"`, r.Host, tc.linkID, query.Encode()))
				}
				fmt.Fprint(w, `[]`)
			}))
			defer server.Close()
			client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Issues(context.Background(), common.WorkbenchConnection{Repository: "a/b"}, nil, 42)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}

func TestGitHubIssueProjectionPreservesNativeFieldsAndStates(t *testing.T) {
	for _, tc := range []struct{ state, reason, want string }{
		{"open", "", "open"}, {"closed", "completed", "done"}, {"closed", "not_planned", "cancelled"},
		{"closed", "duplicate", "cancelled"}, {"closed", "other", "unknown"}, {"closed", "", "unknown"},
	} {
		var issue githubIssue
		payload := fmt.Sprintf(`{"id":123,"number":7,"title":"Task","body":"Description","html_url":"https://github.com/owner/repo/issues/7","state":%q,"state_reason":%q,"updated_at":"2026-01-01T00:00:00Z","labels":[{"name":"bug"}],"assignees":[{"login":"user"}]}`, tc.state, tc.reason)
		if err := json.Unmarshal([]byte(payload), &issue); err != nil {
			t.Fatal(err)
		}
		task := githubIssueTask(common.WorkbenchConnection{ID: "connection", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo"}, issue)
		if task.State != tc.want || task.SourceKey != "issue:123" || task.NativeID != "7" || task.NativeFields["state_reason"] != tc.reason || task.NativeFields["repository"] != "owner/repo" || task.Locator != "https://github.com/owner/repo/issues/7" || len(task.Labels) != 1 || task.Labels[0] != "bug" || len(task.Assignees) != 1 || task.Assignees[0] != "user" {
			t.Errorf("state=%s reason=%s: %+v", tc.state, tc.reason, task)
		}
	}
}

func TestGitHubIssuesPaginationAndIncremental(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Query().Get("state") != "all" || r.URL.Query().Get("since") == "" {
			t.Errorf("request did not preserve authentication and incremental scope")
		}
		if r.URL.Query().Get("page") == "" {
			query := r.URL.Query()
			query.Set("page", "2")
			w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, query.Encode()))
			fmt.Fprint(w, `[{"id":10,"number":1,"title":"old","state":"open","updated_at":"2026-01-01T00:00:00Z"},{"id":20,"number":2,"pull_request":{} }]`)
			return
		}
		fmt.Fprint(w, `[{"id":10,"number":1,"title":"new","state":"closed","state_reason":"not_planned","updated_at":"2026-01-02T00:00:00Z"}]`)
	}))
	defer server.Close()
	client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tasks, err := client.Issues(context.Background(), common.WorkbenchConnection{ID: "c", Kind: common.WorkbenchGitHubRepository, Repository: "a/b"}, &since, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].Title != "new" || tasks[0].State != "cancelled" || tasks[0].SourceKey != "issue:10" {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}
}

func TestGitHubIssuesRejectsPaginationOutsideRepository(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://example.org/repos/other/repo/issues?page=2>; rel="next"`)
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Issues(context.Background(), common.WorkbenchConnection{Repository: "a/b"}, nil, 0)
	if err == nil || !strings.Contains(err.Error(), "pagination") {
		t.Fatalf("expected pagination error, got %v", err)
	}
}

func TestGitHubIssueRejectsRedirectToAnotherRepository(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/a/b/issues/1" {
			http.Redirect(w, r, "/repos/other/repo/issues/1", http.StatusMovedPermanently)
			return
		}
		t.Error("followed redirect into another repository")
		fmt.Fprint(w, `{"id":1,"number":1}`)
	}))
	defer server.Close()
	client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Issue(context.Background(), common.WorkbenchConnection{Repository: "a/b"}, 1); err == nil {
		t.Fatal("accepted issue redirected to another repository")
	}
}

func TestGitHubIssuePollingIntervalIsPerAccountAndConnection(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Poll-Interval", "120")
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	client, err := NewWorkbenchGitHubClient(server.URL, "secret", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	rate := NewWorkbenchGitHubRate(2)
	client.CoordinateRequests(rate, 1)
	if _, err := client.Issues(context.Background(), common.WorkbenchConnection{ID: "repo", Repository: "a/b"}, nil, 0); err != nil {
		t.Fatal(err)
	}
	if delay := rate.PollDelay(1, "repo", time.Now()); delay < 119*time.Second {
		t.Fatalf("poll interval not recorded: %s", delay)
	}
	if rate.PollDelay(1, "other", time.Now()) != 0 || rate.PollDelay(2, "repo", time.Now()) != 0 {
		t.Fatal("poll interval leaked to another source")
	}
}
