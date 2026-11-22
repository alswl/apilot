package services

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

const githubUserCredentialPrefix = "github-user-v1:"

// GitHubAppClientID is the public identifier of the registered GitHub App.
// It is populated by the release build after the app registration is available.
var GitHubAppClientID = "Iv23li9W6zajXFdAUnvh"
var GitHubAppSlug = "apilot-app"

type githubStoredUserCredential struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	Refreshing       bool      `json:"refreshing,omitempty"`
}

func encodeGitHubUserCredential(c integrations.GitHubUserCredential) (string, error) {
	if c.AccessToken == "" || (!c.ExpiresAt.IsZero() && (c.RefreshToken == "" || c.RefreshExpiresAt.IsZero())) {
		return "", common.Err("INVALID_ARGUMENT", "GitHub authorization credentials are incomplete")
	}
	payload, err := json.Marshal(githubStoredUserCredential{AccessToken: c.AccessToken, RefreshToken: c.RefreshToken, ExpiresAt: c.ExpiresAt, RefreshExpiresAt: c.RefreshExpiresAt})
	if err != nil {
		return "", common.Err("INTERNAL", "Could not prepare GitHub credentials")
	}
	return githubUserCredentialPrefix + string(payload), nil
}

type githubCredentialRefresher interface {
	Refresh(context.Context, string) (integrations.GitHubUserCredential, error)
}

// acquireGitHubCredential serializes a connection's complete GitHub request
// cycle with refresh, so a rotated token cannot invalidate an in-flight read.
func (s *Service) acquireGitHubCredential(ctx context.Context, store integrations.WorkbenchCredentialStore, ref string) (string, func(), error) {
	value, _ := s.githubCredentialGates.LoadOrStore(ref, make(chan struct{}, 1))
	gate := value.(chan struct{})
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
	release := sync.OnceFunc(func() { <-gate })
	var refresher githubCredentialRefresher
	if s.githubAppClientID != "" {
		base := s.githubAuthBaseURL
		if base == "" {
			base = "https://github.com"
		}
		client, err := integrations.NewGitHubDeviceClient(base, s.githubAppClientID, s.githubTransport)
		if err != nil {
			release()
			return "", nil, err
		}
		refresher = client
	}
	token, err := readGitHubStoredCredential(ctx, store, ref, refresher, time.Now())
	if err != nil {
		release()
		return "", nil, err
	}
	return token, release, nil
}

func (s *Service) verifyGitHubCredentialInstallation(ctx context.Context, client *integrations.WorkbenchGitHubClient, value string, repositoryID int64) error {
	if !strings.HasPrefix(value, githubUserCredentialPrefix) {
		return nil
	}
	if s.githubAppSlug == "" {
		return common.Err("CONFLICT", "GitHub App installation is not configured")
	}
	return client.VerifyInstallationRepository(ctx, s.githubAppSlug, repositoryID)
}

// Callers must serialize refresh-token exchanges so two repositories cannot rotate the same token.
func readGitHubStoredCredential(ctx context.Context, store integrations.WorkbenchCredentialStore, ref string, refresher githubCredentialRefresher, now time.Time) (string, error) {
	value, err := store.Get(ctx, ref)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(value, githubUserCredentialPrefix) {
		return value, nil
	}
	var stored githubStoredUserCredential
	if json.Unmarshal([]byte(strings.TrimPrefix(value, githubUserCredentialPrefix)), &stored) != nil || stored.AccessToken == "" {
		return "", common.Err("UNAUTHORIZED", "GitHub authorization must be renewed")
	}
	if stored.Refreshing {
		return "", common.Err("UNAUTHORIZED", "GitHub authorization must be renewed")
	}
	if stored.ExpiresAt.IsZero() || stored.ExpiresAt.After(now.Add(time.Minute)) {
		return stored.AccessToken, nil
	}
	if refresher == nil || stored.RefreshToken == "" || stored.RefreshExpiresAt.IsZero() || !stored.RefreshExpiresAt.After(now) {
		return "", common.Err("UNAUTHORIZED", "GitHub authorization must be renewed")
	}
	updater, ok := store.(integrations.WorkbenchCredentialUpdater)
	if !ok {
		return "", common.Err("CONFLICT", "GitHub authorization cannot be refreshed")
	}
	stored.Refreshing = true
	marked, err := json.Marshal(stored)
	if err != nil {
		return "", common.Err("INTERNAL", "Could not prepare GitHub credentials")
	}
	if err := updater.Replace(ctx, ref, githubUserCredentialPrefix+string(marked)); err != nil {
		return "", err
	}
	refreshed, err := refresher.Refresh(ctx, stored.RefreshToken)
	if err != nil {
		return "", common.Err("UNAUTHORIZED", "GitHub authorization must be renewed")
	}
	encoded, err := encodeGitHubUserCredential(refreshed)
	if err != nil {
		return "", err
	}
	if err := updater.Replace(ctx, ref, encoded); err != nil {
		return "", common.Err("UNAUTHORIZED", "GitHub authorization could not be saved; authorize again")
	}
	return refreshed.AccessToken, nil
}
