package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
)

// WriteIssue submits a create or state-change request once. An error after
// dispatch leaves the caller responsible for reconciling the outcome.
func (c *WorkbenchGitHubClient) WriteIssue(ctx context.Context, connection common.WorkbenchConnection, number int64, title, body, state string) (common.WorkbenchTask, error) {
	if c.rate != nil && ctx.Value(githubWriteLease{}) != c {
		var release func()
		var err error
		ctx, release, err = c.AcquireWrite(ctx)
		if err != nil {
			return common.WorkbenchTask{}, err
		}
		defer release()
	}
	parts := strings.Split(connection.Repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return common.WorkbenchTask{}, common.Err("INVALID_ARGUMENT", "Invalid GitHub repository")
	}
	for _, part := range parts {
		if part == "." || part == ".." || strings.ContainsAny(part, "?#%\\ ") {
			return common.WorkbenchTask{}, common.Err("INVALID_ARGUMENT", "Invalid GitHub repository")
		}
	}
	path := "/repos/" + parts[0] + "/" + parts[1] + "/issues"
	method := http.MethodPost
	var payload any
	if number == 0 && title != "" && state == "" {
		payload = struct {
			Title string `json:"title"`
			Body  string `json:"body,omitempty"`
		}{title, body}
	} else if number > 0 && title == "" && body == "" && (state == "open" || state == "done") {
		method = http.MethodPatch
		path += "/" + strconv.FormatInt(number, 10)
		nativeState, reason := "open", ""
		if state == "done" {
			nativeState, reason = "closed", "completed"
		}
		payload = struct {
			State  string `json:"state"`
			Reason string `json:"state_reason,omitempty"`
		}{nativeState, reason}
	} else {
		return common.WorkbenchTask{}, common.Err("INVALID_ARGUMENT", "Invalid GitHub issue operation")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return common.WorkbenchTask{}, err
	}
	u := *c.base
	u.Path = path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return common.WorkbenchTask{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "apilot")
	// Redirecting a mutating request can change the target or its method.
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return common.WorkbenchTask{}, common.Err("SOURCE_UNAVAILABLE", "GitHub write result is unknown")
	}
	defer resp.Body.Close()
	secondaryLimit := githubSecondaryRateLimit(resp)
	if c.rate != nil {
		c.rate.observe(c.accountID, resp, secondaryLimit)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		if resp.StatusCode == http.StatusUnauthorized {
			return common.WorkbenchTask{}, common.Err("UNAUTHORIZED", "GitHub credentials are invalid or expired")
		}
		if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (secondaryLimit || resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0")) {
			return common.WorkbenchTask{}, common.Err("RATE_LIMITED", "GitHub API rate limit reached")
		}
		if resp.StatusCode == http.StatusForbidden {
			return common.WorkbenchTask{}, common.Err("FORBIDDEN", "GitHub denied write permission")
		}
		return common.WorkbenchTask{}, common.Err("SOURCE_UNAVAILABLE", fmt.Sprintf("GitHub write returned HTTP %d", resp.StatusCode))
	}
	var issue githubIssue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil || issue.ID <= 0 || issue.Number <= 0 || (number != 0 && issue.Number != number) {
		return common.WorkbenchTask{}, common.Err("SOURCE_UNAVAILABLE", "GitHub write response could not be verified")
	}
	return githubIssueTask(connection, issue), nil
}
