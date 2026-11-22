package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

const githubAPIVersion = "2026-03-10"

type githubIssueRedirectProbe struct{}

type githubReadStatsKey struct{}

type WorkbenchGitHubReadStats struct {
	Started     time.Time
	Requests    int
	Pages       int
	FirstPageMS int64
	OnPoll      func(int64, time.Time) error
}

func WithWorkbenchGitHubReadStats(ctx context.Context, stats *WorkbenchGitHubReadStats) context.Context {
	return context.WithValue(ctx, githubReadStatsKey{}, stats)
}

func githubReadStats(ctx context.Context) *WorkbenchGitHubReadStats {
	stats, _ := ctx.Value(githubReadStatsKey{}).(*WorkbenchGitHubReadStats)
	return stats
}

func githubSecondaryRateLimit(resp *http.Response) bool {
	if resp.StatusCode != http.StatusForbidden {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return false
	}
	message := bytes.ToLower(body)
	return bytes.Contains(message, []byte("secondary rate limit")) || bytes.Contains(message, []byte("api rate limit exceeded"))
}

// WorkbenchGitHubClient confines credentials to the configured API origin.
type WorkbenchGitHubClient struct {
	base      *url.URL
	token     string
	http      *http.Client
	rate      *WorkbenchGitHubRate
	accountID int64
}

// CoordinateRequests shares quota and serialization across an account's repositories.
func (c *WorkbenchGitHubClient) CoordinateRequests(rate *WorkbenchGitHubRate, accountID int64) {
	c.rate, c.accountID = rate, accountID
}

type WorkbenchGitHubIdentity struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type WorkbenchGitHubRepository struct {
	ID        int64  `json:"id"`
	FullName  string `json:"full_name"`
	HasIssues bool   `json:"has_issues"`
}

func NewWorkbenchGitHubClient(baseURL, token string, transport http.RoundTripper) (*WorkbenchGitHubClient, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") || token == "" {
		return nil, common.Err("INVALID_ARGUMENT", "Invalid GitHub API configuration")
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || !strings.EqualFold(req.URL.Host, base.Host) || req.URL.Scheme != base.Scheme || req.URL.User != nil {
			return common.Err("INVALID_ARGUMENT", "GitHub redirected outside the API origin")
		}
		if len(via) > 0 && via[0].Context().Value(githubIssueRedirectProbe{}) != nil {
			return http.ErrUseLastResponse
		}
		// Same-origin redirects can point to issues transferred out of the repository.
		if len(via) > 0 && strings.Contains(via[0].URL.Path, "/issues") && req.URL.Path != via[0].URL.Path {
			return common.Err("INVALID_ARGUMENT", "GitHub redirected outside the selected issue path")
		}
		return nil
	}
	return &WorkbenchGitHubClient{base: base, token: token, http: client}, nil
}

func (c *WorkbenchGitHubClient) get(ctx context.Context, path string, result any) error {
	u := *c.base
	u.Path = path
	_, err := c.getURL(ctx, &u, result)
	return err
}

func (c *WorkbenchGitHubClient) getURL(ctx context.Context, u *url.URL, result any) (http.Header, error) {
	if u.Scheme != c.base.Scheme || !strings.EqualFold(u.Host, c.base.Host) || u.User != nil {
		return nil, common.Err("INVALID_ARGUMENT", "GitHub URL is outside the API origin")
	}
	if c.rate != nil && ctx.Value(githubWriteLease{}) != c {
		release, err := c.rate.acquire(ctx, c.accountID, false)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", "apilot")
	if stats := githubReadStats(ctx); stats != nil {
		stats.Requests++
	}
	resp, err := c.http.Do(req)
	if err != nil {
		var networkErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkErr) && networkErr.Timeout()) {
			return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub request timed out")
		}
		return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub request failed")
	}
	defer resp.Body.Close()
	secondaryLimit := githubSecondaryRateLimit(resp)
	if c.rate != nil {
		c.rate.observe(c.accountID, resp, secondaryLimit)
	}
	if resp.StatusCode != http.StatusOK {
		switch {
		case (resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect) && ctx.Value(githubIssueRedirectProbe{}) != nil:
			problem := common.Err("SOURCE_REDIRECT", "GitHub redirected an issue")
			problem.Details["location"] = resp.Header.Get("Location")
			return nil, problem
		case resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (secondaryLimit || resp.Header.Get("Retry-After") != "" || resp.Header.Get("X-RateLimit-Remaining") == "0")):
			problem := common.Err("RATE_LIMITED", "GitHub API rate limit reached")
			problem.Details["retry_after"] = resp.Header.Get("Retry-After")
			problem.Details["rate_limit_reset"] = resp.Header.Get("X-RateLimit-Reset")
			return nil, problem
		case resp.StatusCode == http.StatusUnauthorized:
			return nil, common.Err("UNAUTHORIZED", "GitHub credentials are invalid or expired")
		case resp.StatusCode == http.StatusForbidden:
			return nil, common.Err("FORBIDDEN", "GitHub access is not permitted")
		case resp.StatusCode == http.StatusGone && ctx.Value(githubIssueRedirectProbe{}) != nil:
			return nil, common.Err("ISSUE_DELETED", "GitHub issue was deleted")
		case resp.StatusCode == http.StatusNotFound:
			return nil, common.Err("NOT_FOUND", "GitHub resource is unavailable")
		default:
			return nil, common.Err("SOURCE_UNAVAILABLE", fmt.Sprintf("GitHub returned HTTP %d", resp.StatusCode))
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub returned an invalid response")
	}
	return resp.Header, nil
}

// Identity returns the authenticated account without exposing its credential.
func (c *WorkbenchGitHubClient) Identity(ctx context.Context) (WorkbenchGitHubIdentity, error) {
	var identity WorkbenchGitHubIdentity
	err := c.get(ctx, "/user", &identity)
	return identity, err
}

// Repository resolves an owner/repository to its stable GitHub identity.
func (c *WorkbenchGitHubClient) Repository(ctx context.Context, name string) (WorkbenchGitHubRepository, error) {
	var repository WorkbenchGitHubRepository
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return repository, common.Err("INVALID_ARGUMENT", "Repository must use owner/name")
	}
	for _, part := range parts {
		if part == "." || part == ".." || strings.ContainsAny(part, "?#%\\ ") {
			return repository, common.Err("INVALID_ARGUMENT", "Invalid GitHub repository")
		}
	}
	err := c.get(ctx, "/repos/"+parts[0]+"/"+parts[1], &repository)
	return repository, err
}
