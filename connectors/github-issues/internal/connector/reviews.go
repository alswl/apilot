package connector

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/alswl/apilot/connectors/protocol"
)

type githubUser struct {
	ID int64 `json:"id"`
}

type githubPull struct {
	Number    int64        `json:"number"`
	Title     string       `json:"title"`
	State     string       `json:"state"`
	User      githubUser   `json:"user"`
	Reviewers []githubUser `json:"requested_reviewers"`
	Base      struct {
		Repo repository `json:"repo"`
	} `json:"base"`
}

func (c *Connector) reviewRequests(ctx context.Context, scope protocol.ScopeContext) (any, *protocol.RPCError) {
	if !positiveID(scope.ScopeRef) {
		return nil, protocol.Problem(protocol.InvalidConfig, "scope", "Invalid GitHub repository")
	}
	token, problem := c.token(ctx, scope.AccountRef, scope.EndpointRef)
	if problem != nil {
		return nil, problem
	}
	client := newAPI(token, c.transport)
	var user githubUser
	if _, problem := client.get(ctx, "/user", nil, &user); problem != nil {
		return nil, problem
	}
	if user.ID <= 0 || strconv.FormatInt(user.ID, 10) != scope.AccountRef {
		return nil, renewAuthorization()
	}
	repos, problem := client.installationRepositories(ctx, "pull_requests")
	if problem != nil {
		return nil, problem
	}
	i := slices.IndexFunc(repos, func(repo repository) bool { return strconv.FormatInt(repo.ID, 10) == scope.ScopeRef })
	if i < 0 {
		return nil, protocol.Problem(protocol.AccessDenied, "scope", "GitHub Pull requests read access is required")
	}
	return client.reviewRequests(ctx, repos[i], user.ID)
}

func (c *apiClient) reviewRequests(ctx context.Context, repo repository, userID int64) (any, *protocol.RPCError) {
	result := protocol.GitHubReviewRequestsResult{Items: []protocol.GitHubReviewRequest{}}
	path := "/repositories/" + strconv.FormatInt(repo.ID, 10) + "/pulls"
	problem := c.pages(ctx, path, func(q url.Values) (http.Header, *protocol.RPCError) {
		// GitHub's default state is open. Check each returned PR as well.
		var pulls []githubPull
		header, problem := c.get(ctx, path, q, &pulls)
		if problem != nil {
			if problem.Data.Diagnostic == "404" {
				problem = protocol.Problem(protocol.AccessDenied, "scope", "GitHub Pull requests read access is required")
			}
			return header, problem
		}
		for _, pull := range pulls {
			if pull.State != "open" || pull.User.ID == userID ||
				!slices.Contains(pull.Reviewers, githubUser{ID: userID}) {
				continue
			}
			if pull.Base.Repo.ID != repo.ID || pull.Number <= 0 || strings.TrimSpace(pull.Title) == "" {
				return header, protocol.Problem(protocol.Temporary, "scope", "Invalid GitHub pull request")
			}
			result.Items = append(result.Items, protocol.GitHubReviewRequest{Repository: repo.FullName, Number: pull.Number, Title: pull.Title})
		}
		return header, nil
	}, "/repos/"+repo.FullName+"/pulls")
	if problem != nil {
		return nil, problem
	}
	return result, nil
}
