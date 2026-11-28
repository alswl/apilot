package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/alswl/apilot/core/pkg/common"
)

type githubIssue struct {
	ID          int64           `json:"id"`
	Number      int64           `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	HTMLURL     string          `json:"html_url"`
	State       string          `json:"state"`
	StateReason string          `json:"state_reason"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees []struct {
		Login string `json:"login"`
	} `json:"assignees"`
}

func githubIssueTask(connection common.WorkbenchConnection, issue githubIssue) common.WorkbenchTask {
	state := "unknown"
	switch {
	case issue.State == "open":
		state = "open"
	case issue.State == "closed" && issue.StateReason == "completed":
		state = "done"
	case issue.State == "closed" && (issue.StateReason == "not_planned" || issue.StateReason == "duplicate"):
		state = "cancelled"
	}
	task := common.WorkbenchTask{
		ConnectionID: connection.ID,
		Kind:         connection.Kind,
		Mode:         connection.Mode,
		SourceKey:    "issue:" + strconv.FormatInt(issue.ID, 10),
		NativeID:     strconv.FormatInt(issue.Number, 10),
		Title:        issue.Title,
		Summary:      issue.Body,
		Locator:      issue.HTMLURL,
		State:        state,
		NativeFields: map[string]string{
			"repository":   connection.Repository,
			"issue_id":     strconv.FormatInt(issue.ID, 10),
			"state":        issue.State,
			"state_reason": issue.StateReason,
			"updated_at":   issue.UpdatedAt.UTC().Format(time.RFC3339Nano),
		},
	}
	for _, label := range issue.Labels {
		task.Labels = append(task.Labels, label.Name)
	}
	for _, assignee := range issue.Assignees {
		task.Assignees = append(task.Assignees, assignee.Login)
	}
	return task
}

// Issue reads an individual issue using its repository-local number.
func (c *WorkbenchGitHubClient) Issue(ctx context.Context, connection common.WorkbenchConnection, number int64) (common.WorkbenchTask, error) {
	if number <= 0 {
		return common.WorkbenchTask{}, common.Err("INVALID_ARGUMENT", "Invalid issue number")
	}
	parts, ok := GitHubRepositoryParts(connection.Repository)
	if !ok {
		return common.WorkbenchTask{}, common.Err("INVALID_ARGUMENT", "Invalid repository")
	}
	var issue githubIssue
	path := "/repos/" + parts[0] + "/" + parts[1] + "/issues/" + strconv.FormatInt(number, 10)
	ctx = context.WithValue(ctx, githubIssueRedirectProbe{}, true)
	if err := c.get(ctx, path, &issue); err != nil {
		var problem *common.Error
		if errors.As(err, &problem) && problem.Code == "SOURCE_REDIRECT" {
			rawLocation, _ := problem.Details["location"].(string)
			location, parseErr := url.Parse(rawLocation)
			if parseErr == nil {
				base := *c.base
				base.Path = path
				target := base.ResolveReference(location)
				segments := strings.Split(strings.TrimPrefix(target.Path, "/"), "/")
				if target.Scheme == c.base.Scheme && strings.EqualFold(target.Host, c.base.Host) && target.User == nil && len(segments) == 5 && segments[0] == "repos" && segments[3] == "issues" && segments[1] != "" && segments[2] != "" && (!strings.EqualFold(segments[1], parts[0]) || !strings.EqualFold(segments[2], parts[1])) {
					if n, err := strconv.ParseInt(segments[4], 10, 64); err == nil && n > 0 {
						return common.WorkbenchTask{}, common.Err("ISSUE_MOVED", "GitHub issue redirected to another repository")
					}
				}
			}
		}
		return common.WorkbenchTask{}, err
	}
	if issue.ID <= 0 || issue.Number != number || (len(issue.PullRequest) != 0 && string(issue.PullRequest) != "null") {
		return common.WorkbenchTask{}, common.Err("SOURCE_UNAVAILABLE", "GitHub returned an unexpected issue")
	}
	return githubIssueTask(connection, issue), nil
}

// GitHubRepositoryParts validates the owner/repository form used by GitHub REST paths.
func GitHubRepositoryParts(repository string) ([]string, bool) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, false
	}
	for _, part := range parts {
		if part == "." || part == ".." || strings.ContainsAny(part, "?#%\\") || strings.IndexFunc(part, unicode.IsSpace) >= 0 {
			return nil, false
		}
	}
	return parts, true
}

// Issues reads all pages of a repository snapshot or an incremental window.
func (c *WorkbenchGitHubClient) Issues(ctx context.Context, connection common.WorkbenchConnection, since *time.Time, repositoryID int64) ([]common.WorkbenchTask, error) {
	parts := strings.Split(connection.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, common.Err("INVALID_ARGUMENT", "Repository must use owner/name")
	}
	for _, part := range parts {
		if part == "." || part == ".." || strings.ContainsAny(part, "?#%\\ ") {
			return nil, common.Err("INVALID_ARGUMENT", "Invalid GitHub repository")
		}
	}
	path := "/repos/" + parts[0] + "/" + parts[1] + "/issues"
	u := *c.base
	u.Path = path
	query := url.Values{"state": {"all"}, "per_page": {"100"}, "direction": {"asc"}, "sort": {"created"}}
	if since != nil {
		query.Set("sort", "updated")
		query.Set("since", since.UTC().Format(time.RFC3339))
	}
	u.RawQuery = query.Encode()
	seenPages := make(map[string]bool)
	issues := make(map[int64]githubIssue)
	order := make([]int64, 0)
	for {
		if seenPages[u.String()] {
			return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub pagination contains a cycle")
		}
		seenPages[u.String()] = true
		var page []githubIssue
		stats := githubReadStats(ctx)
		if stats != nil {
			stats.Pages++
		}
		header, err := c.getURL(ctx, &u, &page)
		if err != nil {
			return nil, err
		}
		if stats != nil && stats.Pages == 1 {
			stats.FirstPageMS = time.Since(stats.Started).Milliseconds()
		}
		if c.rate != nil {
			until := c.rate.observePoll(c.accountID, connection.ID, header)
			if !until.IsZero() && stats != nil && stats.OnPoll != nil {
				if err := stats.OnPoll(c.accountID, until); err != nil {
					return nil, err
				}
			}
		}
		for _, issue := range page {
			if len(issue.PullRequest) != 0 && string(issue.PullRequest) != "null" {
				continue
			}
			if issue.ID <= 0 || issue.Number <= 0 {
				return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub returned an issue without an identity")
			}
			previous, exists := issues[issue.ID]
			if !exists {
				order = append(order, issue.ID)
			}
			if !exists || issue.UpdatedAt.After(previous.UpdatedAt) {
				issues[issue.ID] = issue
			}
		}
		next := ""
		for _, link := range strings.Split(header.Get("Link"), ",") {
			fields := strings.Split(link, ";")
			if len(fields) < 2 || strings.TrimSpace(fields[1]) != `rel="next"` {
				continue
			}
			next = strings.Trim(strings.TrimSpace(fields[0]), "<>")
		}
		if next == "" {
			break
		}
		nextURL, err := url.Parse(next)
		if err != nil {
			return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub returned an invalid pagination URL")
		}
		nextURL = u.ResolveReference(nextURL)
		canonicalPath := ""
		if repositoryID > 0 {
			canonicalPath = "/repositories/" + strconv.FormatInt(repositoryID, 10) + "/issues"
		}
		if nextURL.Scheme != c.base.Scheme || !strings.EqualFold(nextURL.Host, c.base.Host) || nextURL.User != nil || (nextURL.Path != path && (canonicalPath == "" || nextURL.Path != canonicalPath)) {
			return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub pagination left the selected repository")
		}
		nextQuery := nextURL.Query()
		for _, key := range []string{"state", "per_page", "direction", "sort", "since"} {
			if len(nextQuery[key]) != len(query[key]) || nextQuery.Get(key) != query.Get(key) {
				return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub pagination changed the issue query")
			}
		}
		u = *nextURL
	}
	tasks := make([]common.WorkbenchTask, 0, len(order))
	for _, id := range order {
		issue := issues[id]
		task := githubIssueTask(connection, issue)
		tasks = append(tasks, task)
	}
	return tasks, nil
}
