package integrations

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
)

func (c *WorkbenchGitHubClient) ReviewRequests(ctx context.Context, repo WorkbenchGitHubRepository, accountID int64) ([]common.WorkbenchGitHubReviewRequest, error) {
	path := fmt.Sprintf("/repositories/%d/pulls", repo.ID)
	var result []common.WorkbenchGitHubReviewRequest
	err := c.githubPages(ctx, path, "/repos/"+repo.FullName+"/pulls", func(page *url.URL) (http.Header, error) {
		var pulls []struct {
			Number    int64                     `json:"number"`
			Title     string                    `json:"title"`
			State     string                    `json:"state"`
			User      WorkbenchGitHubIdentity   `json:"user"`
			Reviewers []WorkbenchGitHubIdentity `json:"requested_reviewers"`
			Base      struct {
				Repo WorkbenchGitHubRepository `json:"repo"`
			} `json:"base"`
		}
		header, err := c.getURL(ctx, page, &pulls)
		if err != nil {
			return nil, err
		}
		for _, pull := range pulls {
			// GitHub's default state is open. Use only outstanding direct requests.
			if pull.State != "open" || pull.User.ID == accountID ||
				!slices.ContainsFunc(pull.Reviewers, func(user WorkbenchGitHubIdentity) bool { return user.ID == accountID }) {
				continue
			}
			if pull.Base.Repo.ID != repo.ID || pull.Number <= 0 || strings.TrimSpace(pull.Title) == "" {
				return nil, common.Err("SOURCE_UNAVAILABLE", "GitHub returned an invalid pull request")
			}
			result = append(result, common.WorkbenchGitHubReviewRequest{Repository: repo.FullName, Number: pull.Number, Title: pull.Title})
		}
		return header, nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *WorkbenchGitHubClient) VerifyPullRequestsInstallation(ctx context.Context, appSlug string, repositoryID int64) error {
	found, err := c.visitInstallationRepositories(ctx, appSlug, "pull_requests", func(repo WorkbenchGitHubRepository) bool { return repo.ID == repositoryID })
	if err != nil || found {
		return err
	}
	return common.Err("FORBIDDEN", "GitHub Pull requests read access is required")
}
