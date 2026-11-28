package services

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
)

type githubReviewScope struct {
	id, repository, credential, access   string
	parent, endpoint, release, authState string
	accountID, repositoryID              int64
	version, authGeneration              int64
}

func (s *Service) githubReviewScopes() ([]githubReviewScope, error) {
	connections, err := s.WorkbenchConnections()
	if err != nil {
		return nil, err
	}
	var scopes []githubReviewScope
	for _, connection := range connections {
		if connection.Paused || connection.PluginDisabled {
			continue
		}
		scope := githubReviewScope{id: connection.ID, version: connection.Version, access: connection.AccessState}
		switch connection.Kind {
		case common.WorkbenchGitHubRepository:
			store, ok := s.store.(workbenchGitHubStore)
			if !ok {
				return nil, common.Err("INTERNAL", "GitHub connection storage unavailable")
			}
			progress, err := store.WorkbenchGitHubProgress(scope.id)
			if err != nil {
				return nil, err
			}
			if progress.AccountID != connection.GitHubAccountID {
				return nil, common.Err("STALE_REVISION", "GitHub connection changed")
			}
			scope.repository, scope.accountID = connection.Repository, progress.AccountID
			scope.repositoryID, scope.credential = progress.RepositoryID, progress.CredentialRef
		case "github-issues":
			store, err := s.managedConnectorStore()
			if err != nil {
				return nil, err
			}
			stored, err := store.WorkbenchConnectorScope(scope.id)
			if err != nil {
				return nil, err
			}
			connector := s.registeredConnector(connection.Kind)
			if connector == nil || stored.Config.Version != scope.version {
				return nil, common.Err("STALE_REVISION", "GitHub connection changed")
			}
			scope.parent, scope.endpoint = stored.ConnectionID, stored.Context.EndpointRef
			scope.authState, scope.authGeneration = stored.AuthState, stored.AuthGeneration
			scope.release = connector.release.PackageDigest
			scope.accountID, _ = strconv.ParseInt(stored.Context.AccountRef, 10, 64)
			scope.repositoryID, _ = strconv.ParseInt(stored.Context.ScopeRef, 10, 64)
		default:
			continue
		}
		if scope.accountID <= 0 || scope.repositoryID <= 0 {
			return nil, common.Err("UNAUTHORIZED", "Verify the GitHub connection")
		}
		scopes = append(scopes, scope)
	}
	return scopes, nil
}

// WorkbenchGitHubReviewRequests reads a fresh projection for the connected account.
func (s *Service) WorkbenchGitHubReviewRequests(ctx context.Context) (common.WorkbenchGitHubReviewRequests, error) {
	var result common.WorkbenchGitHubReviewRequests
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	scopes, err := s.githubReviewScopes()
	if err != nil {
		return result, err
	}
	if len(scopes) == 0 {
		return result, common.Err("UNAUTHORIZED", "Connect a GitHub repository to see review requests")
	}
	accountID := scopes[0].accountID
	for _, scope := range scopes {
		if scope.accountID != accountID {
			return result, common.Err("CONFLICT", "GitHub review requests require a single connected account")
		}
	}
	result.Items = []common.WorkbenchGitHubReviewRequest{}
	type reviewKey struct{ repositoryID, number int64 }
	seen := map[reviewKey]bool{}
	var firstErr error
	read := 0
	for _, scope := range scopes {
		if ctx.Err() != nil {
			firstErr = ctx.Err()
			break
		}
		if delay := s.githubRate.BackoffDelay(accountID, time.Now()); delay > 0 {
			firstErr = common.Err("RATE_LIMITED", "GitHub API rate limit reached")
			break
		}
		items, err := s.readGitHubReviewScope(ctx, scope)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			if common.ProtocolError(err).Code == "RATE_LIMITED" {
				break
			}
			continue
		}
		read++
		for _, item := range items {
			key := reviewKey{scope.repositoryID, item.Number}
			if !seen[key] {
				result.Items = append(result.Items, item)
				seen[key] = true
			}
		}
	}
	if read == 0 {
		return common.WorkbenchGitHubReviewRequests{}, firstErr
	}
	current, err := s.githubReviewScopes()
	if err != nil {
		return common.WorkbenchGitHubReviewRequests{}, err
	}
	if !slices.Equal(scopes, current) {
		return common.WorkbenchGitHubReviewRequests{}, common.Err("STALE_REVISION", "GitHub connection changed")
	}
	result.Incomplete = read < len(scopes)
	slices.SortFunc(result.Items, func(a, b common.WorkbenchGitHubReviewRequest) int {
		if order := strings.Compare(strings.ToLower(a.Repository), strings.ToLower(b.Repository)); order != 0 {
			return order
		}
		return cmp.Compare(b.Number, a.Number)
	})
	return result, nil
}

func (s *Service) readGitHubReviewScope(ctx context.Context, scope githubReviewScope) ([]common.WorkbenchGitHubReviewRequest, error) {
	if scope.parent != "" {
		return s.readManagedGitHubReviewScope(ctx, scope)
	}
	credentials, err := s.workbenchGitHubCredentials()
	if err != nil {
		return nil, err
	}
	token, release, err := s.acquireGitHubCredential(ctx, credentials, scope.credential)
	if err != nil {
		if common.ProtocolError(err).Code == "NOT_FOUND" {
			return nil, common.Err("UNAUTHORIZED", "GitHub authorization is required")
		}
		return nil, err
	}
	defer release()
	client, err := s.workbenchGitHubClient(token)
	if err != nil {
		return nil, err
	}
	client.CoordinateRequests(s.githubRate, scope.accountID)
	identity, err := client.Identity(ctx)
	if err != nil {
		return nil, err
	}
	if identity.ID != scope.accountID {
		return nil, common.Err("UNAUTHORIZED", "GitHub account changed")
	}
	repo, err := client.Repository(ctx, scope.repository)
	if err != nil {
		return nil, err
	}
	if repo.ID != scope.repositoryID || !strings.EqualFold(repo.FullName, scope.repository) {
		return nil, common.Err("FORBIDDEN", "GitHub repository changed")
	}
	value, err := credentials.Get(ctx, scope.credential)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(value, githubUserCredentialPrefix) {
		if err := client.VerifyPullRequestsInstallation(ctx, s.githubAppSlug, repo.ID); err != nil {
			return nil, err
		}
	}
	return client.ReviewRequests(ctx, repo, identity.ID)
}

func (s *Service) readManagedGitHubReviewScope(ctx context.Context, scope githubReviewScope) ([]common.WorkbenchGitHubReviewRequest, error) {
	if scope.authState != "authorized" {
		return nil, common.Err("UNAUTHORIZED", "GitHub authorization is required")
	}
	connector := s.registeredConnector("github-issues")
	if connector == nil || connector.release.PackageDigest != scope.release ||
		!slices.Contains(connector.description.Capabilities, protocol.GitHubReviewRequests) {
		return nil, common.Err("SOURCE_UNAVAILABLE", "The GitHub connector does not support review requests")
	}
	store, err := s.managedConnectorStore()
	if err != nil {
		return nil, err
	}
	if until, err := store.WorkbenchConnectorBackoff(scope.parent, scope.id, []string{"github-api"}); err != nil {
		return nil, err
	} else if until != nil && time.Now().Before(*until) {
		return nil, common.Err("RATE_LIMITED", "GitHub API rate limit reached")
	}
	params := protocol.ScopeContext{AccountRef: strconv.FormatInt(scope.accountID, 10), EndpointRef: scope.endpoint, ScopeRef: strconv.FormatInt(scope.repositoryID, 10)}
	result, err := callWorkbenchConnector[protocol.GitHubReviewRequestsResult](ctx, connector.process, common.ID(), protocol.GitHubReviewRequests, params, nil)
	if err != nil {
		var problem *protocol.RPCError
		if errors.As(err, &problem) && problem.Data.Kind == protocol.RateLimited && problem.Data.RetryAt != nil {
			s.githubRate.RecordBackoff(scope.accountID, *problem.Data.RetryAt)
		}
		return nil, publicConnectorError(err)
	}
	var items []common.WorkbenchGitHubReviewRequest
	for _, item := range result.Items {
		items = append(items, common.WorkbenchGitHubReviewRequest(item))
	}
	return items, nil
}
