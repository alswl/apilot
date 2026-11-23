package services

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func (s *Service) gitHubAuthorizationManager() (*githubAuthorizationManager, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return nil, common.Err("CONFLICT", "App is stopping")
	}
	if s.githubAuthorization != nil {
		return s.githubAuthorization, nil
	}
	if s.githubAppClientID == "" {
		return nil, common.Err("CONFLICT", "GitHub browser authorization is not configured")
	}
	if !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`).MatchString(s.githubAppSlug) {
		return nil, common.Err("CONFLICT", "GitHub browser authorization is not configured")
	}
	base := s.githubAuthBaseURL
	if base == "" {
		base = "https://github.com"
	}
	flow, err := integrations.NewGitHubDeviceClient(base, s.githubAppClientID, s.githubTransport)
	if err != nil {
		return nil, err
	}
	s.githubAuthorization = &githubAuthorizationManager{flow: flow, changed: func() { s.volatileRevision.Add(1) }, installationURL: "https://github.com/apps/" + url.PathEscape(s.githubAppSlug) + "/installations/new"}
	return s.githubAuthorization, nil
}

func (s *Service) StartGitHubAuthorization() (GitHubAuthorizationStatus, error) {
	manager, err := s.gitHubAuthorizationManager()
	if err != nil {
		return GitHubAuthorizationStatus{}, err
	}
	return manager.begin(s.ctx)
}

func (s *Service) GitHubAuthorizationStatus(id string) (GitHubAuthorizationStatus, error) {
	manager, err := s.gitHubAuthorizationManager()
	if err != nil {
		return GitHubAuthorizationStatus{}, err
	}
	return manager.status(id)
}

func (s *Service) CancelGitHubAuthorization(id string) error {
	manager, err := s.gitHubAuthorizationManager()
	if err != nil {
		return err
	}
	return manager.cancel(id)
}

func (s *Service) ListGitHubRepositories(authorizationID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	var token string
	if authorizationID != "" {
		manager, err := s.gitHubAuthorizationManager()
		if err != nil {
			return nil, err
		}
		credential, err := manager.authorizedCredential(authorizationID)
		if err != nil {
			return nil, common.Err("UNAUTHORIZED", "GitHub authorization is required")
		}
		token = credential.AccessToken
	} else {
		credentials, err := s.workbenchGitHubCredentials()
		if err != nil {
			return nil, err
		}
		ref, err := s.reusableGitHubCredential(ctx, credentials)
		if err != nil {
			return nil, err
		}
		if ref == "" {
			return nil, common.Err("UNAUTHORIZED", "GitHub authorization is required")
		}
		var release func()
		token, release, err = s.acquireGitHubCredential(ctx, credentials, ref)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	client, err := s.workbenchGitHubClient(token)
	if err != nil {
		return nil, err
	}
	names, err := client.ListInstallationRepositories(ctx, s.githubAppSlug)
	slices.Sort(names)
	return slices.Compact(names), err
}

func (s *Service) ConfigureWorkbenchGitHubAuthorized(c common.WorkbenchConnection, id string, writeEnabled *bool) (string, error) {
	manager, err := s.gitHubAuthorizationManager()
	if err != nil {
		return "", err
	}
	credential, err := manager.claim(id)
	if err != nil {
		return "", err
	}
	saved := false
	defer func() { manager.finish(id, saved) }()
	stored, err := encodeGitHubUserCredential(credential)
	if err != nil {
		return "", err
	}
	connectionID, err := s.configureWorkbenchGitHub(c, credential.AccessToken, stored, writeEnabled)
	saved = err == nil
	return connectionID, err
}

// GitHubAuthorizationStatus contains only information safe for the local UI.
type GitHubAuthorizationStatus struct {
	ID              string    `json:"id"`
	State           string    `json:"state"`
	VerificationURI string    `json:"verification_uri,omitempty"`
	UserCode        string    `json:"user_code,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	InstallationURL string    `json:"installation_url,omitempty"`
}

type githubDeviceFlow interface {
	Begin(context.Context) (integrations.GitHubDeviceAuthorization, error)
	Poll(context.Context, integrations.GitHubDeviceAuthorization) (integrations.GitHubUserCredential, error)
}

type githubAuthorizationSession struct {
	status     GitHubAuthorizationStatus
	credential integrations.GitHubUserCredential
	cancel     context.CancelFunc
	expiry     *time.Timer
}

// githubAuthorizationManager keeps device codes and credentials in memory only.
// A manager has one active attempt; replacement and shutdown erase its secrets.
type githubAuthorizationManager struct {
	mu              sync.Mutex
	flow            githubDeviceFlow
	installationURL string
	current         *githubAuthorizationSession
	closed          bool
	changed         func()
}

func (m *githubAuthorizationManager) notifyChange() {
	if m.changed != nil {
		m.changed()
	}
}

func (m *githubAuthorizationManager) begin(ctx context.Context) (GitHubAuthorizationStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return GitHubAuthorizationStatus{}, common.Err("CONFLICT", "GitHub authorization is unavailable")
	}
	if m.current != nil {
		return GitHubAuthorizationStatus{}, common.Err("CONFLICT", "GitHub authorization is already in progress")
	}
	attempt, err := m.flow.Begin(ctx)
	if err != nil {
		return GitHubAuthorizationStatus{}, err
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	session := &githubAuthorizationSession{status: GitHubAuthorizationStatus{ID: common.ID(), State: "waiting", VerificationURI: attempt.VerificationURI, UserCode: attempt.UserCode, ExpiresAt: attempt.ExpiresAt, InstallationURL: m.installationURL}, cancel: cancel}
	m.current = session
	m.notifyChange()
	session.expiry = time.AfterFunc(time.Until(attempt.ExpiresAt), func() { m.expire(session) })
	go func() {
		credential, pollErr := m.flow.Poll(pollCtx, attempt)
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.current != session || session.status.State != "waiting" {
			return
		}
		defer m.notifyChange()
		session.status.UserCode = ""
		switch {
		case pollErr == nil:
			session.credential = credential
			session.status.State = "authorized"
		case errors.Is(pollErr, context.Canceled):
			session.status.State = "cancelled"
		default:
			session.status.State = "failed"
			var problem *common.Error
			if errors.As(pollErr, &problem) {
				if problem.Code == "AUTHORIZATION_DENIED" {
					session.status.State = "denied"
				}
				if problem.Code == "AUTHORIZATION_EXPIRED" {
					session.status.State = "expired"
				}
			}
		}
	}()
	return session.status, nil
}

func (m *githubAuthorizationManager) expire(session *githubAuthorizationSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == session {
		session.cancel()
		session.credential = integrations.GitHubUserCredential{}
		session.status.UserCode = ""
		session.status.State = "expired"
		m.notifyChange()
	}
}

func (m *githubAuthorizationManager) status(id string) (GitHubAuthorizationStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.status.ID != id {
		return GitHubAuthorizationStatus{}, common.Err("NOT_FOUND", "GitHub authorization was not found")
	}
	return m.current.status, nil
}

func (m *githubAuthorizationManager) authorizedCredential(id string) (integrations.GitHubUserCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readyCredentialLocked(id)
}

// claim reserves a completed credential for one connection attempt.
func (m *githubAuthorizationManager) claim(id string) (integrations.GitHubUserCredential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	credential, err := m.readyCredentialLocked(id)
	if err == nil {
		m.current.status.State = "saving"
		m.notifyChange()
	}
	return credential, err
}

func (m *githubAuthorizationManager) readyCredentialLocked(id string) (integrations.GitHubUserCredential, error) {
	if m.current == nil || m.current.status.ID != id || m.current.status.State != "authorized" || !time.Now().Before(m.current.status.ExpiresAt) {
		return integrations.GitHubUserCredential{}, common.Err("CONFLICT", "GitHub authorization is not ready")
	}
	return m.current.credential, nil
}

func (m *githubAuthorizationManager) finish(id string, saved bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.status.ID != id || m.current.status.State != "saving" {
		return
	}
	defer m.notifyChange()
	if !saved && time.Now().Before(m.current.status.ExpiresAt) {
		m.current.status.State = "authorized"
		return
	}
	m.current.expiry.Stop()
	m.current.cancel()
	m.current.credential = integrations.GitHubUserCredential{}
	m.current = nil
}

func (m *githubAuthorizationManager) cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == nil || m.current.status.ID != id {
		return common.Err("NOT_FOUND", "GitHub authorization was not found")
	}
	m.current.cancel()
	m.current.expiry.Stop()
	m.current.credential = integrations.GitHubUserCredential{}
	m.current = nil
	m.notifyChange()
	return nil
}

func (m *githubAuthorizationManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.current != nil {
		m.current.cancel()
		m.current.expiry.Stop()
		m.current.credential = integrations.GitHubUserCredential{}
		m.current = nil
	}
}
