package integrations

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
)

// VerifyInstallationRepository verifies that a GitHub App user token can use
// the selected repository through a non-suspended installation of this app.
func (c *WorkbenchGitHubClient) VerifyInstallationRepository(ctx context.Context, appSlug string, repositoryID int64) error {
	if appSlug == "" || repositoryID <= 0 {
		return common.Err("INVALID_ARGUMENT", "GitHub App installation is not configured")
	}
	found, err := c.visitInstallationRepositories(ctx, appSlug, func(repo WorkbenchGitHubRepository) bool { return repo.ID == repositoryID })
	if err != nil || found {
		return err
	}
	return common.Err("FORBIDDEN", "Install the GitHub App and grant access to this repository")
}

func (c *WorkbenchGitHubClient) ListInstallationRepositories(ctx context.Context, appSlug string) ([]string, error) {
	names := []string{}
	_, err := c.visitInstallationRepositories(ctx, appSlug, func(repo WorkbenchGitHubRepository) bool {
		if repo.ID > 0 && repo.HasIssues && repo.FullName != "" {
			names = append(names, repo.FullName)
		}
		return false
	})
	return names, err
}

func (c *WorkbenchGitHubClient) visitInstallationRepositories(ctx context.Context, appSlug string, visit func(WorkbenchGitHubRepository) bool) (bool, error) {
	type installation struct {
		ID          int64   `json:"id"`
		Slug        string  `json:"app_slug"`
		SuspendedAt *string `json:"suspended_at"`
	}
	var installations []installation
	err := c.installationPages(ctx, "/user/installations", func(pageURL *url.URL) (http.Header, error) {
		var page struct {
			Installations []installation `json:"installations"`
		}
		header, err := c.getURL(ctx, pageURL, &page)
		installations = append(installations, page.Installations...)
		return header, err
	})
	if err != nil {
		return false, err
	}
	for _, install := range installations {
		if install.ID <= 0 || install.SuspendedAt != nil || !strings.EqualFold(install.Slug, appSlug) {
			continue
		}
		var repositories []WorkbenchGitHubRepository
		path := fmt.Sprintf("/user/installations/%d/repositories", install.ID)
		err := c.installationPages(ctx, path, func(pageURL *url.URL) (http.Header, error) {
			var page struct {
				Repositories []WorkbenchGitHubRepository `json:"repositories"`
			}
			header, err := c.getURL(ctx, pageURL, &page)
			repositories = append(repositories, page.Repositories...)
			return header, err
		})
		if err != nil {
			return false, err
		}
		for _, repository := range repositories {
			if visit(repository) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (c *WorkbenchGitHubClient) installationPages(ctx context.Context, path string, read func(*url.URL) (http.Header, error)) error {
	current := *c.base
	current.Path = path
	current.RawQuery = url.Values{"per_page": {"100"}}.Encode()
	seen := make(map[string]bool)
	for {
		if seen[current.String()] {
			return common.Err("SOURCE_UNAVAILABLE", "GitHub installation pagination contains a cycle")
		}
		seen[current.String()] = true
		header, err := read(&current)
		if err != nil {
			return err
		}
		next := ""
		for _, link := range strings.Split(header.Get("Link"), ",") {
			fields := strings.Split(link, ";")
			if len(fields) > 1 && strings.TrimSpace(fields[1]) == `rel="next"` {
				next = strings.Trim(strings.TrimSpace(fields[0]), "<>")
			}
		}
		if next == "" {
			return nil
		}
		parsed, err := url.Parse(next)
		if err != nil {
			return common.Err("SOURCE_UNAVAILABLE", "GitHub installation pagination is invalid")
		}
		parsed = current.ResolveReference(parsed)
		if parsed.Scheme != c.base.Scheme || !strings.EqualFold(parsed.Host, c.base.Host) || parsed.User != nil || parsed.Path != path || parsed.Query().Get("per_page") != "100" {
			return common.Err("SOURCE_UNAVAILABLE", "GitHub installation pagination left the selected scope")
		}
		current = *parsed
	}
}
