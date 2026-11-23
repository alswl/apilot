package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

const apiHost = "api.github.com"

type apiClient struct {
	http  *http.Client
	token string
	usage protocol.Usage
}

func newAPI(token string, transport http.RoundTripper) *apiClient {
	return &apiClient{token: token, http: &http.Client{Transport: transport, Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *apiClient) get(ctx context.Context, path string, query url.Values, out any) (http.Header, *protocol.RPCError) {
	target := url.URL{Scheme: "https", Host: apiHost, Path: path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Invalid GitHub request")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "apilot-github-issues")
	c.usage.Requests++
	res, err := c.http.Do(req)
	if err != nil {
		return nil, protocol.Problem(protocol.Temporary, "scope", "GitHub request failed")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusOK {
		if json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out) != nil {
			return nil, protocol.Problem(protocol.Temporary, "scope", "Invalid GitHub response")
		}
		return res.Header, nil
	}
	secondary := false
	if res.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
		lower := bytes.ToLower(body)
		secondary = bytes.Contains(lower, []byte("secondary rate limit")) || bytes.Contains(lower, []byte("api rate limit exceeded"))
	}
	switch {
	case res.StatusCode == http.StatusTooManyRequests || (res.StatusCode == http.StatusForbidden && (secondary || res.Header.Get("Retry-After") != "" || res.Header.Get("X-RateLimit-Remaining") == "0")):
		c.usage.RateLimits++
		problem := protocol.Problem(protocol.RateLimited, "account", "GitHub rate limit reached")
		problem.Data.RateLimitBucket = "github-api"
		retry := time.Now().Add(time.Minute)
		if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds >= 0 && seconds <= 86400 {
			retry = time.Now().Add(time.Duration(seconds) * time.Second)
		}
		if epoch, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && res.Header.Get("X-RateLimit-Remaining") == "0" && time.Unix(epoch, 0).After(retry) {
			retry = time.Unix(epoch, 0)
		}
		problem.Data.RetryAt = &retry
		return res.Header, problem
	case res.StatusCode == http.StatusUnauthorized:
		return res.Header, renewAuthorization()
	case res.StatusCode == http.StatusForbidden:
		return res.Header, protocol.Problem(protocol.AccessDenied, "scope", "GitHub access was denied")
	default:
		problem := protocol.Problem(protocol.Temporary, "scope", "GitHub resource could not be verified")
		problem.Data.Diagnostic = strconv.Itoa(res.StatusCode)
		return res.Header, problem
	}
}

type repository struct {
	ID        int64  `json:"id"`
	FullName  string `json:"full_name"`
	HasIssues bool   `json:"has_issues"`
}

func (c *Connector) options(ctx context.Context, p protocol.OptionsParams) (any, *protocol.RPCError) {
	if p.Field != "repository_id" {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Unknown GitHub option field")
	}
	offset := 0
	if p.Page != "" {
		var err error
		offset, err = strconv.Atoi(p.Page)
		if err != nil || offset <= 0 {
			return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Invalid option page")
		}
	}
	token, problem := c.token(ctx, p.AccountRef, p.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	repos, problem := client.repositories(ctx)
	if problem != nil {
		return nil, problem
	}
	var matched []repository
	for _, repo := range repos {
		if strings.Contains(strings.ToLower(repo.FullName), strings.ToLower(p.Search)) {
			matched = append(matched, repo)
		}
	}
	if offset > len(matched) {
		offset = len(matched)
	}
	result := protocol.ConfigOptions{Items: []protocol.ConfigOption{}}
	for _, repo := range matched[offset:min(offset+50, len(matched))] {
		result.Items = append(result.Items, protocol.ConfigOption{Value: strconv.FormatInt(repo.ID, 10), Label: repo.FullName})
	}
	if offset+50 < len(matched) {
		result.NextPage = strconv.Itoa(offset + 50)
	}
	return result, nil
}

func (c *apiClient) repositories(ctx context.Context) ([]repository, *protocol.RPCError) {
	type install struct {
		ID          int64   `json:"id"`
		Slug        string  `json:"app_slug"`
		SuspendedAt *string `json:"suspended_at"`
	}
	var installs []install
	problem := c.pages(ctx, "/user/installations", func(q url.Values) (http.Header, *protocol.RPCError) {
		var payload struct {
			Installations []install `json:"installations"`
		}
		header, err := c.get(ctx, "/user/installations", q, &payload)
		installs = append(installs, payload.Installations...)
		return header, err
	})
	if problem != nil {
		return nil, problem
	}
	byID := map[int64]repository{}
	for _, in := range installs {
		if in.ID <= 0 || in.SuspendedAt != nil || !strings.EqualFold(in.Slug, appSlug) {
			continue
		}
		path := fmt.Sprintf("/user/installations/%d/repositories", in.ID)
		problem := c.pages(ctx, path, func(q url.Values) (http.Header, *protocol.RPCError) {
			var payload struct {
				Repositories []repository `json:"repositories"`
			}
			header, err := c.get(ctx, path, q, &payload)
			for _, repo := range payload.Repositories {
				if repo.ID > 0 && repo.HasIssues && repo.FullName != "" {
					byID[repo.ID] = repo
				}
			}
			return header, err
		})
		if problem != nil {
			return nil, problem
		}
	}
	list := make([]repository, 0, len(byID))
	for _, repo := range byID {
		list = append(list, repo)
	}
	slices.SortFunc(list, func(a, b repository) int {
		if n := strings.Compare(a.FullName, b.FullName); n != 0 {
			return n
		}
		return strings.Compare(strconv.FormatInt(a.ID, 10), strconv.FormatInt(b.ID, 10))
	})
	return list, nil
}

func (c *apiClient) pages(ctx context.Context, path string, read func(url.Values) (http.Header, *protocol.RPCError), aliases ...string) *protocol.RPCError {
	query := url.Values{"per_page": {"100"}}
	for page := 1; page <= 1000; page++ {
		query.Set("page", strconv.Itoa(page))
		header, problem := read(query)
		if problem != nil {
			return problem
		}
		next, err := nextPage(header, path, query, aliases...)
		if err != nil {
			return err
		}
		if next == 0 {
			return nil
		}
		if next != page+1 {
			break
		}
	}
	return protocol.Problem(protocol.Temporary, "scope", "Invalid GitHub pagination sequence")
}

func nextPage(header http.Header, path string, query url.Values, aliases ...string) (int, *protocol.RPCError) {
	for _, link := range strings.Split(header.Get("Link"), ",") {
		parts := strings.Split(link, ";")
		if len(parts) < 2 || strings.TrimSpace(parts[1]) != `rel="next"` {
			continue
		}
		raw := strings.Trim(strings.TrimSpace(parts[0]), "<>")
		link, err := url.Parse(raw)
		if err == nil && link.User == nil && (link.Host == "" || (link.Scheme == "https" && strings.EqualFold(link.Host, apiHost))) && (link.Path == path || slices.Contains(aliases, link.Path)) {
			values := link.Query()
			valid := true
			for k, v := range query {
				if k != "page" && !slices.Equal(values[k], v) {
					valid = false
				}
			}
			page, err := strconv.Atoi(values.Get("page"))
			if valid && err == nil && page > 0 {
				return page, nil
			}
		}
		return 0, protocol.Problem(protocol.Temporary, "scope", "GitHub pagination left the selected query")
	}
	return 0, nil
}

func (c *Connector) validate(ctx context.Context, p protocol.ValidateParams) (any, *protocol.RPCError) {
	var scope struct {
		RepositoryID string `json:"repository_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(p.ScopeConfig))
	decoder.DisallowUnknownFields()
	var endpointConfig map[string]json.RawMessage
	if p.ScopeSchemaVersion != 1 || p.EndpointSchemaVersion != 1 || p.EndpointRef != endpoint || decoder.Decode(&scope) != nil ||
		!positiveID(scope.RepositoryID) || json.Unmarshal(p.EndpointConfig, &endpointConfig) != nil || endpointConfig == nil || len(endpointConfig) != 0 {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Select a valid GitHub repository")
	}
	if p.Mode == "static" {
		return protocol.ValidatedScope{ScopeRef: scope.RepositoryID, EndpointRef: endpoint, Label: scope.RepositoryID}, nil
	}
	if p.Mode != "access" {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Invalid validation mode")
	}
	token, problem := c.token(ctx, p.AccountRef, p.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	repos, problem := client.repositories(ctx)
	if problem != nil {
		return nil, problem
	}
	for _, repo := range repos {
		if strconv.FormatInt(repo.ID, 10) == scope.RepositoryID {
			return protocol.ValidatedScope{ScopeRef: scope.RepositoryID, EndpointRef: endpoint, Label: repo.FullName}, nil
		}
	}
	return nil, protocol.Problem(protocol.AccessDenied, "scope", "The GitHub App cannot access this repository")
}
