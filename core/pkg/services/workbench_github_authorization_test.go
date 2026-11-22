package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type fakeGitHubDeviceFlow struct {
	result  chan integrations.GitHubUserCredential
	stopped chan struct{}
}

func (f fakeGitHubDeviceFlow) Begin(context.Context) (integrations.GitHubDeviceAuthorization, error) {
	return integrations.GitHubDeviceAuthorization{DeviceCode: "device-secret", UserCode: "CODE", VerificationURI: "https://github.com/login/device", ExpiresAt: time.Now().Add(time.Minute), Interval: time.Second}, nil
}
func (f fakeGitHubDeviceFlow) Poll(ctx context.Context, _ integrations.GitHubDeviceAuthorization) (integrations.GitHubUserCredential, error) {
	select {
	case value := <-f.result:
		return value, nil
	case <-ctx.Done():
		close(f.stopped)
		return integrations.GitHubUserCredential{}, ctx.Err()
	}
}
func TestGitHubAuthorizationSessionKeepsSecretsPrivateAndSingleUse(t *testing.T) {
	flow := fakeGitHubDeviceFlow{result: make(chan integrations.GitHubUserCredential, 1), stopped: make(chan struct{})}
	manager := &githubAuthorizationManager{flow: flow}
	defer manager.close()
	status, err := manager.begin(context.Background())
	if err != nil || status.State != "waiting" || status.UserCode != "CODE" {
		t.Fatalf("begin: %+v %v", status, err)
	}
	if _, err := manager.begin(context.Background()); err == nil {
		t.Fatal("started concurrent authorization")
	}
	flow.result <- integrations.GitHubUserCredential{AccessToken: "access-secret", RefreshToken: "refresh-secret"}
	for deadline := time.Now().Add(time.Second); ; time.Sleep(time.Millisecond) {
		status, err = manager.status(status.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "authorized" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("authorization did not finish")
		}
	}
	encoded, err := json.Marshal(status)
	if err != nil || strings.Contains(string(encoded), "secret") || status.UserCode != "" {
		t.Fatalf("unsafe status: %s %v", encoded, err)
	}
	credential, err := manager.claim(status.ID)
	if err != nil || credential.AccessToken != "access-secret" || credential.RefreshToken != "refresh-secret" {
		t.Fatalf("take failed: %v", err)
	}
	if _, err := manager.claim(status.ID); err == nil {
		t.Fatal("credential consumed twice")
	}
	manager.finish(status.ID, false)
	if _, err := manager.claim(status.ID); err != nil {
		t.Fatalf("failed save could not retry: %v", err)
	}
	manager.finish(status.ID, true)
	if _, err := manager.claim(status.ID); err == nil {
		t.Fatal("saved credential could be used twice")
	}
}
func TestGitHubAuthorizationCancellationStopsPolling(t *testing.T) {
	flow := fakeGitHubDeviceFlow{result: make(chan integrations.GitHubUserCredential), stopped: make(chan struct{})}
	manager := &githubAuthorizationManager{flow: flow}
	defer manager.close()
	status, err := manager.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.cancel(status.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-flow.stopped:
	case <-time.After(time.Second):
		t.Fatal("polling not cancelled")
	}
	if _, err := manager.status(status.ID); err == nil {
		t.Fatal("cancelled authorization still visible")
	}
}

func TestGitHubAuthorizedConnectionStoresPrivateCredential(t *testing.T) {
	var installed atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access-secret" {
			t.Error("wrong authorization")
		}
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":7,"login":"user"}`)
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
		case "/repos/owner/second":
			fmt.Fprint(w, `{"id":43,"full_name":"owner/second","has_issues":true}`)
		case "/repos/owner/repo/issues", "/repos/owner/second/issues":
			fmt.Fprint(w, `[]`)
		case "/user/installations":
			if !installed.Load() {
				fmt.Fprint(w, `{"installations":[]}`)
				return
			}
			fmt.Fprint(w, `{"installations":[{"id":10,"app_slug":"apilot-test"}]}`)
		case "/user/installations/10/repositories":
			fmt.Fprint(w, `{"repositories":[{"id":43,"full_name":"owner/second","has_issues":true},{"id":42,"full_name":"owner/repo","has_issues":true},{"id":44,"full_name":"owner/disabled","has_issues":false}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	credentials := &memoryGitHubCredentials{values: make(map[string]string)}
	s.githubCredentials, s.githubBaseURL, s.githubTransport = credentials, server.URL, server.Client().Transport
	s.githubAppSlug = "apilot-test"
	flow := fakeGitHubDeviceFlow{result: make(chan integrations.GitHubUserCredential, 1), stopped: make(chan struct{})}
	s.githubAuthorization = &githubAuthorizationManager{flow: flow}
	status, err := s.StartGitHubAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	flow.result <- integrations.GitHubUserCredential{AccessToken: "access-secret", RefreshToken: "refresh-secret", ExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(24 * time.Hour)}
	for deadline := time.Now().Add(time.Second); ; time.Sleep(time.Millisecond) {
		status, err = s.GitHubAuthorizationStatus(status.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "authorized" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("authorization did not finish")
		}
	}
	if _, err := s.ConfigureWorkbenchGitHubAuthorized(common.WorkbenchConnection{Name: "Repo", Repository: "owner/repo"}, status.ID, nil); err == nil {
		t.Fatal("uninstalled repository was accepted")
	}
	status, err = s.GitHubAuthorizationStatus(status.ID)
	if err != nil || status.State != "authorized" {
		t.Fatalf("failed save lost authorization: %+v %v", status, err)
	}
	installed.Store(true)
	if names, err := s.ListGitHubRepositories(status.ID); err != nil || !slices.Equal(names, []string{"owner/repo", "owner/second"}) {
		t.Fatalf("authorized repository choices: %v, %v", names, err)
	}
	id, err := s.ConfigureWorkbenchGitHubAuthorized(common.WorkbenchConnection{Name: "Repo", Repository: "owner/repo"}, status.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := s.store.(workbenchGitHubStore).WorkbenchGitHubProgress(id)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := credentials.Get(context.Background(), progress.CredentialRef)
	if err != nil || !strings.HasPrefix(stored, githubUserCredentialPrefix) || !strings.Contains(stored, "refresh-secret") {
		t.Fatalf("protected credential not stored: %v", err)
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	public, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(public), "access-secret") || strings.Contains(string(public), "refresh-secret") {
		t.Fatal("credential leaked through snapshot")
	}
	if _, err := s.ConfigureWorkbenchGitHubAuthorized(common.WorkbenchConnection{Name: "Second", Repository: "owner/repo"}, status.ID, nil); err == nil {
		t.Fatal("reused authorization session")
	}
	second, err := s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Name: "Second", Repository: "owner/second"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := s.store.(workbenchGitHubStore).WorkbenchGitHubProgress(second)
	if err != nil || shared.CredentialRef != progress.CredentialRef {
		t.Fatalf("second repository did not reuse the credential: %v", err)
	}
	if err := s.RemoveWorkbench(id); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Get(context.Background(), shared.CredentialRef); err != nil {
		t.Fatalf("removing one repository deleted the shared credential: %v", err)
	}
}

func TestGitHubAppInstallationRevocationHidesSnapshot(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
		case "/user/installations":
			fmt.Fprint(w, `{"installations":[]}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	encoded, err := encodeGitHubUserCredential(integrations.GitHubUserCredential{AccessToken: "app-token"})
	if err != nil {
		t.Fatal(err)
	}
	credentials := &memoryGitHubCredentials{values: map[string]string{"ref": encoded}}
	s.githubCredentials, s.githubBaseURL, s.githubTransport, s.githubAppSlug = credentials, server.URL, server.Client().Transport, "apilot-test"
	store := s.store.(interface {
		workbenchGitHubStore
		workbenchGitHubSyncStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := common.WorkbenchTask{ID: "private-task", ConnectionID: c.ID, SourceKey: "issue:1", Title: "Private", State: "open"}
	if err := store.CommitWorkbenchGitHubSync(c, nil, nil, []common.WorkbenchTask{task}, now, 0, now, true); err != nil {
		t.Fatal(err)
	}
	attempt, err := s.RunWorkbenchSync(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		snapshot, err := s.WorkbenchSnapshot(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].ID == attempt && snapshot.Attempts[0].State == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sync did not finish")
		}
	}
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || progress.AccessState != "unavailable" || progress.Cursor != nil {
		t.Fatalf("revocation not applied: %+v %v", progress, err)
	}
	if _, err := s.WorkbenchTask(task.ID); err == nil {
		t.Fatal("revoked task remains visible")
	}
}
