package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/integrations"
)

type oauthMemoryStore struct {
	value     string
	writes    int
	failWrite int
}

func (m *oauthMemoryStore) Put(context.Context, string, string) error {
	return errors.New("unexpected insert")
}
func (m *oauthMemoryStore) Get(context.Context, string) (string, error) { return m.value, nil }
func (m *oauthMemoryStore) Delete(context.Context, string) error        { return nil }
func (m *oauthMemoryStore) List(context.Context) ([]string, error)      { return nil, nil }
func (m *oauthMemoryStore) Replace(_ context.Context, _, value string) error {
	m.writes++
	if m.failWrite == m.writes {
		return errors.New("storage failed")
	}
	m.value = value
	return nil
}

type oauthFakeRefresher struct {
	calls int
	fail  bool
	value integrations.GitHubUserCredential
}

func (f *oauthFakeRefresher) Refresh(context.Context, string) (integrations.GitHubUserCredential, error) {
	f.calls++
	if f.fail {
		return integrations.GitHubUserCredential{}, errors.New("network failed")
	}
	return f.value, nil
}

func TestGitHubStoredCredentialRefreshAndRecovery(t *testing.T) {
	now := time.Now()
	old := integrations.GitHubUserCredential{AccessToken: "old-access", RefreshToken: "old-refresh", ExpiresAt: now.Add(-time.Minute), RefreshExpiresAt: now.Add(time.Hour)}
	encoded, err := encodeGitHubUserCredential(old)
	if err != nil {
		t.Fatal(err)
	}
	newCred := integrations.GitHubUserCredential{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(2 * time.Hour)}
	t.Run("refresh", func(t *testing.T) {
		store := &oauthMemoryStore{value: encoded}
		refresher := &oauthFakeRefresher{value: newCred}
		token, err := readGitHubStoredCredential(context.Background(), store, "ref", refresher, now)
		if err != nil || token != "new-access" || refresher.calls != 1 || store.writes != 2 || strings.Contains(store.value, "old-refresh") {
			t.Fatalf("refresh failed: %v", err)
		}
		token, err = readGitHubStoredCredential(context.Background(), store, "ref", refresher, now)
		if err != nil || token != "new-access" || refresher.calls != 1 {
			t.Fatalf("rotated token not reused: %v", err)
		}
	})
	t.Run("lost response does not retry", func(t *testing.T) {
		store := &oauthMemoryStore{value: encoded}
		refresher := &oauthFakeRefresher{fail: true}
		if _, err := readGitHubStoredCredential(context.Background(), store, "ref", refresher, now); err == nil {
			t.Fatal("expected failure")
		}
		if _, err := readGitHubStoredCredential(context.Background(), store, "ref", refresher, now); err == nil || refresher.calls != 1 {
			t.Fatalf("replayed uncertain refresh: %v", err)
		}
	})
	t.Run("failed storage after rotation does not retry", func(t *testing.T) {
		store := &oauthMemoryStore{value: encoded, failWrite: 2}
		refresher := &oauthFakeRefresher{value: newCred}
		if _, err := readGitHubStoredCredential(context.Background(), store, "ref", refresher, now); err == nil {
			t.Fatal("expected failure")
		}
		if _, err := readGitHubStoredCredential(context.Background(), store, "ref", refresher, now); err == nil || refresher.calls != 1 {
			t.Fatalf("replayed refresh after failed save: %v", err)
		}
	})
	t.Run("legacy PAT", func(t *testing.T) {
		store := &oauthMemoryStore{value: "legacy-pat"}
		token, err := readGitHubStoredCredential(context.Background(), store, "ref", nil, now)
		if err != nil || token != "legacy-pat" || store.writes != 0 {
			t.Fatalf("legacy credential: %v", err)
		}
	})
}

func TestGitHubCredentialGateWaitIsCancellableAndIndependent(t *testing.T) {
	s := &Service{}
	store := &oauthMemoryStore{value: "legacy-pat"}
	_, release, err := s.acquireGitHubCredential(context.Background(), store, "one")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := s.acquireGitHubCredential(ctx, store, "one"); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting did not cancel: %v", err)
	}
	_, other, err := s.acquireGitHubCredential(context.Background(), store, "two")
	if err != nil {
		t.Fatalf("other connection was blocked: %v", err)
	}
	other()
}
