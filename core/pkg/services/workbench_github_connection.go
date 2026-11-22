package services

import (
	"context"
	"encoding/json"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type workbenchGitHubStore interface {
	WorkbenchGitHubProgress(string) (common.WorkbenchGitHubProgress, error)
	SaveWorkbenchGitHub(common.WorkbenchConnection, int64, common.WorkbenchGitHubProgress) error
}

func (s *Service) cleanupWorkbenchGitHubCredentials(ctx context.Context, credentials integrations.WorkbenchCredentialStore) error {
	s.githubCredentialMu.Lock()
	defer s.githubCredentialMu.Unlock()
	store, ok := s.store.(interface{ WorkbenchGitHubCredentialRefs() ([]string, error) })
	if !ok {
		return nil
	}
	refs, err := store.WorkbenchGitHubCredentialRefs()
	if err != nil {
		return err
	}
	listed, err := credentials.List(ctx)
	if err != nil {
		return err
	}
	active := make(map[string]bool, len(refs))
	for _, ref := range refs {
		active[ref] = true
	}
	for _, ref := range listed {
		if !active[ref] {
			if err := credentials.Delete(ctx, ref); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) workbenchGitHubCredentials() (integrations.WorkbenchCredentialStore, error) {
	if s.githubCredentials != nil {
		return s.githubCredentials, nil
	}
	namespace, err := integrations.WorkbenchCredentialNamespace(s.dir)
	if err != nil {
		return nil, err
	}
	return integrations.NewWorkbenchKeychain(namespace), nil
}

// The caller holds githubCredentialMu while changing the set of references.
func (s *Service) deleteUnusedGitHubCredential(credentials integrations.WorkbenchCredentialStore, ref string) {
	if store, ok := s.store.(interface{ WorkbenchGitHubCredentialRefs() ([]string, error) }); ok {
		refs, err := store.WorkbenchGitHubCredentialRefs()
		if err != nil || slices.Contains(refs, ref) {
			return
		}
	}
	_ = credentials.Delete(context.Background(), ref)
}

func (s *Service) reusableGitHubCredential(ctx context.Context, credentials integrations.WorkbenchCredentialStore) (string, error) {
	store, ok := s.store.(interface {
		WorkbenchGitHubCredentialAccounts() (map[string]int64, error)
	})
	if !ok {
		return "", nil
	}
	refs, err := store.WorkbenchGitHubCredentialAccounts()
	if err != nil {
		return "", err
	}
	candidate := ""
	var account int64
	var bestExpiry time.Time
	now := time.Now()
	for ref, accountID := range refs {
		if accountID <= 0 {
			continue
		}
		value, err := credentials.Get(ctx, ref)
		if err != nil {
			if common.ProtocolError(err).Code == "NOT_FOUND" {
				continue
			}
			return "", err
		}
		payload, browserCredential := strings.CutPrefix(value, githubUserCredentialPrefix)
		var stored githubStoredUserCredential
		if !browserCredential || json.Unmarshal([]byte(payload), &stored) != nil || stored.AccessToken == "" || stored.Refreshing {
			continue
		}
		expiry := stored.ExpiresAt
		if !expiry.IsZero() && !expiry.After(now.Add(time.Minute)) && (stored.RefreshToken == "" || !stored.RefreshExpiresAt.After(now)) {
			continue
		}
		if account != 0 && account != accountID {
			return "", nil
		}
		account = accountID
		if candidate == "" || expiry.After(bestExpiry) || (expiry.Equal(bestExpiry) && ref < candidate) {
			candidate, bestExpiry = ref, expiry
		}
	}
	return candidate, nil
}

func (s *Service) workbenchGitHubClient(token string) (*integrations.WorkbenchGitHubClient, error) {
	base := s.githubBaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	return integrations.NewWorkbenchGitHubClient(base, token, s.githubTransport)
}

// ConfigureWorkbenchGitHub verifies and stores one GitHub repository connection.
func (s *Service) ConfigureWorkbenchGitHub(c common.WorkbenchConnection, token string, writeEnabled *bool) (string, error) {
	return s.configureWorkbenchGitHub(c, token, token, writeEnabled)
}

func (s *Service) configureWorkbenchGitHub(c common.WorkbenchConnection, token, storedValue string, writeEnabled *bool) (string, error) {
	store, ok := s.store.(workbenchGitHubStore)
	if !ok {
		return "", common.Err("INTERNAL", "GitHub connection storage unavailable")
	}
	c.Name = strings.TrimSpace(c.Name)
	c.Repository = strings.TrimSpace(c.Repository)
	if c.Name == "" || len(c.Name) > 256 {
		return "", common.Err("INVALID_ARGUMENT", "Connection name is required")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	s.githubCredentialMu.Lock()
	defer s.githubCredentialMu.Unlock()
	credentials, err := s.workbenchGitHubCredentials()
	if err != nil {
		return "", err
	}
	reuseRef := ""
	if token == "" {
		if c.ID != "" {
			progress, err := store.WorkbenchGitHubProgress(c.ID)
			if err != nil {
				return "", err
			}
			reuseRef = progress.CredentialRef
		} else {
			reuseRef, err = s.reusableGitHubCredential(ctx, credentials)
			if err != nil {
				return "", err
			}
		}
		if reuseRef == "" {
			return "", common.Err("UNAUTHORIZED", "GitHub authorization is required")
		}
		var release func()
		token, release, err = s.acquireGitHubCredential(ctx, credentials, reuseRef)
		if err != nil {
			return "", err
		}
		defer release()
	}
	client, err := s.workbenchGitHubClient(token)
	if err != nil {
		return "", err
	}
	identity, err := client.Identity(ctx)
	if err != nil {
		return "", err
	}
	client.CoordinateRequests(s.githubRate, identity.ID)
	credentialValue := storedValue
	if reuseRef != "" {
		credentialValue, err = credentials.Get(ctx, reuseRef)
		if err != nil {
			return "", err
		}
	}
	repository, err := client.Repository(ctx, c.Repository)
	if err != nil {
		if strings.HasPrefix(credentialValue, githubUserCredentialPrefix) && common.ProtocolError(err).Code == "NOT_FOUND" {
			problem := common.Err("FORBIDDEN", "Check the repository name and grant the GitHub App access")
			problem.Details["github_installation_required"] = true
			return "", problem
		}
		return "", err
	}
	if identity.ID <= 0 || repository.ID <= 0 || !repository.HasIssues {
		return "", common.Err("INVALID_ARGUMENT", "GitHub repository identity or Issues capability is unavailable")
	}
	if err := s.verifyGitHubCredentialInstallation(ctx, client, credentialValue, repository.ID); err != nil {
		if problem := common.ProtocolError(err); problem.Code == "FORBIDDEN" {
			problem.Details["github_installation_required"] = true
			return "", problem
		}
		return "", err
	}
	if c.ID != "" {
		old, err := s.WorkbenchSnapshot(c.ID)
		if err != nil {
			return "", err
		}
		if old.Kind != common.WorkbenchGitHubRepository || old.Version != c.Version {
			return "", common.Err("STALE_REVISION", "Connection configuration changed")
		}
	}
	ref := reuseRef
	if ref == "" {
		ref = common.ID()
		if err := credentials.Put(ctx, ref, storedValue); err != nil {
			return "", err
		}
	}
	saved := false
	oldRef := ""
	defer func() {
		if !saved && reuseRef == "" {
			_ = credentials.Delete(context.Background(), ref)
		} else if saved && oldRef != "" && oldRef != ref {
			s.deleteUnusedGitHubCredential(credentials, oldRef)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return "", common.Err("CONFLICT", "App is stopping")
	}
	if err := s.workbenchPluginAvailable(common.WorkbenchGitHubRepository); err != nil {
		return "", err
	}
	connections, err := s.WorkbenchConnections()
	if err != nil {
		return "", err
	}
	for _, connection := range connections {
		if connection.ID != c.ID && connection.Kind == common.WorkbenchGitHubRepository {
			if existing, err := store.WorkbenchGitHubProgress(connection.ID); err != nil {
				return "", err
			} else if existing.RepositoryID == repository.ID {
				problem := common.Err("CONFLICT", "Repository is already connected")
				problem.Details["connection_id"] = connection.ID
				return "", problem
			}
		}
		if connection.ID != c.ID && strings.EqualFold(connection.Name, c.Name) {
			return "", common.Err("INVALID_ARGUMENT", "Connection name must be unique")
		}
	}
	expectedVersion := c.Version
	if c.ID == "" {
		c.ID = common.ID()
		c.Version = 1
	} else {
		old, err := s.WorkbenchSnapshot(c.ID)
		if err != nil {
			return "", err
		}
		if old.Kind != common.WorkbenchGitHubRepository || old.Version != expectedVersion {
			return "", common.Err("STALE_REVISION", "Connection configuration changed")
		}
		progress, err := store.WorkbenchGitHubProgress(c.ID)
		if err != nil {
			return "", err
		}
		oldRef = progress.CredentialRef
		c.Version++
		c.Paused = old.Paused
	}
	c.Kind = common.WorkbenchGitHubRepository
	c.Repository = repository.FullName
	c.Mode = "live"
	if c.ID != "" && expectedVersion > 0 {
		old, err := s.WorkbenchSnapshot(c.ID)
		if err != nil {
			return "", err
		}
		c.WriteEnabled = old.WriteEnabled
	}
	if writeEnabled != nil {
		c.WriteEnabled = *writeEnabled
	}
	progress := common.WorkbenchGitHubProgress{
		ConnectionID: c.ID, RepositoryID: repository.ID, AccountID: identity.ID,
		AccountLogin: identity.Login, CredentialRef: ref, AccessState: "pending",
	}
	if err := store.SaveWorkbenchGitHub(c, expectedVersion, progress); err != nil {
		return "", err
	}
	saved = true
	if active := s.workbenchActive[c.ID]; active != nil {
		s.workbenchDirty[c.ID] = true
		active()
	}
	if !c.Paused {
		workbench, err := s.workbenchStore()
		if err != nil {
			log.Printf("workbench: saved GitHub connection but could not schedule sync: %v", err)
		} else if err := s.startWorkbenchLocked(workbench, c.ID, "configuration"); err != nil {
			log.Printf("workbench: saved GitHub connection but could not schedule sync: %v", err)
		}
	}
	return c.ID, nil
}
