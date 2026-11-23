package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/dal"
	"github.com/alswl/apilot/core/pkg/integrations"
)

type memoryGitHubCredentials struct {
	mu     sync.Mutex
	values map[string]string
}

func TestGitHubCredentialReuseAfterReauthorization(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store := s.store.(workbenchGitHubStore)
	credentials := &memoryGitHubCredentials{values: map[string]string{}}
	ctx := context.Background()
	add := func(ref string, repo, account int64, expiry time.Time) {
		t.Helper()
		c := common.WorkbenchConnection{ID: ref, Kind: common.WorkbenchGitHubRepository, Version: 1, Paused: true}
		if err := store.SaveWorkbenchGitHub(c, 0, common.WorkbenchGitHubProgress{
			ConnectionID: c.ID, RepositoryID: repo, AccountID: account, CredentialRef: ref, AccessState: "pending",
		}); err != nil {
			t.Fatal(err)
		}
		value, err := encodeGitHubUserCredential(integrations.GitHubUserCredential{
			AccessToken: "fixture-access", RefreshToken: "fixture-refresh", ExpiresAt: expiry, RefreshExpiresAt: expiry.Add(time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := credentials.Put(ctx, ref, value); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	add("initial", 1, 1, now.Add(time.Hour))
	add("renewed", 2, 1, now.Add(2*time.Hour))
	add("expired", 3, 2, now.Add(-2*time.Hour))
	if ref, err := s.reusableGitHubCredential(ctx, credentials); err != nil || ref != "renewed" {
		t.Fatalf("expected the renewed credential for the existing account, got %q (%v)", ref, err)
	}
	add("another-account", 4, 2, now.Add(3*time.Hour))
	if ref, err := s.reusableGitHubCredential(ctx, credentials); err != nil || ref != "" {
		t.Fatalf("expected explicit authorization for multiple accounts, got %q (%v)", ref, err)
	}
}

type failOnceGitHubCredentials struct {
	*memoryGitHubCredentials
	failMu  sync.Mutex
	failRef string
}

type failGitHubScanStore struct{ *dal.Store }

type blockingGitHubCredentials struct {
	*memoryGitHubCredentials
	stored  chan struct{}
	release chan struct{}
}

func (m *blockingGitHubCredentials) Put(ctx context.Context, ref, value string) error {
	if err := m.memoryGitHubCredentials.Put(ctx, ref, value); err != nil {
		return err
	}
	close(m.stored)
	select {
	case <-m.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *failGitHubScanStore) RecordWorkbench(c common.WorkbenchConnection, attempts []common.WorkbenchAttempt, before, after []common.WorkbenchTask, success *time.Time) error {
	if c.Kind == common.WorkbenchGitHubRepository && len(attempts) > 0 && attempts[0].State == "running" {
		return common.Err("SOURCE_UNAVAILABLE", "Injected scheduling failure")
	}
	return s.Store.RecordWorkbench(c, attempts, before, after, success)
}

func (m *failOnceGitHubCredentials) Delete(ctx context.Context, ref string) error {
	m.failMu.Lock()
	if ref == m.failRef {
		m.failRef = ""
		m.failMu.Unlock()
		return common.Err("SOURCE_UNAVAILABLE", "Injected cleanup failure")
	}
	m.failMu.Unlock()
	return m.memoryGitHubCredentials.Delete(ctx, ref)
}

func (m *memoryGitHubCredentials) Put(_ context.Context, ref, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[ref] = value
	return nil
}
func (m *memoryGitHubCredentials) Get(_ context.Context, ref string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value, ok := m.values[ref]; ok {
		return value, nil
	}
	return "", common.Err("NOT_FOUND", "Credential not found")
}
func (m *memoryGitHubCredentials) Delete(_ context.Context, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, ref)
	return nil
}
func (m *memoryGitHubCredentials) List(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var refs []string
	for ref := range m.values {
		refs = append(refs, ref)
	}
	return refs, nil
}

func TestGitHubCredentialRotationCleanupAndRemoval(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":1,"login":"user"}`)
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
		case "/repos/owner/repo/issues":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	credentials := &memoryGitHubCredentials{values: map[string]string{"orphan": "unused"}}
	s.githubCredentials, s.githubBaseURL, s.githubTransport = credentials, server.URL, server.Client().Transport
	id, err := s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Name: "Repo", Repository: "owner/repo"}, "first-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || bytes.Contains(encoded, []byte("first-token")) {
		t.Fatalf("connection exposed credential: %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("first-token")) {
			t.Fatalf("credential stored in %s", file.Name())
		}
	}
	_, err = s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Name: "Repo", Repository: "owner/repo"}, "first-token", nil)
	var duplicate *common.Error
	if !errors.As(err, &duplicate) || duplicate.Code != "CONFLICT" || duplicate.Details["connection_id"] != id {
		t.Fatalf("duplicate repository did not identify existing connection: %v", err)
	}
	store := s.store.(workbenchGitHubStore)
	first, err := store.WorkbenchGitHubProgress(id)
	if err != nil {
		t.Fatal(err)
	}
	c := common.WorkbenchConnection{ID: id, Version: 1, Name: "Repo", Repository: "owner/repo"}
	if _, err := s.ConfigureWorkbenchGitHub(c, "", nil); err != nil {
		t.Fatal(err)
	}
	unchanged, err := store.WorkbenchGitHubProgress(id)
	if err != nil || unchanged.CredentialRef != first.CredentialRef {
		t.Fatalf("editing without token rotated credential: %+v %v", unchanged, err)
	}
	c.Version = 2
	if _, err := s.ConfigureWorkbenchGitHub(c, "second-token", nil); err != nil {
		t.Fatal(err)
	}
	rotated, err := store.WorkbenchGitHubProgress(id)
	if err != nil || rotated.CredentialRef == first.CredentialRef {
		t.Fatalf("credential not rotated: %+v %v", rotated, err)
	}
	if _, err := credentials.Get(context.Background(), first.CredentialRef); err == nil {
		t.Fatal("old credential remains")
	}
	if err := s.cleanupWorkbenchGitHubCredentials(context.Background(), credentials); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Get(context.Background(), "orphan"); err == nil {
		t.Fatal("orphan credential remains")
	}
	if _, err := credentials.Get(context.Background(), rotated.CredentialRef); err != nil {
		t.Fatal("active credential was removed")
	}
	c.Version = 3
	results := make(chan error, 2)
	for _, token := range []string{"third-token", "fourth-token"} {
		go func(token string) { _, err := s.ConfigureWorkbenchGitHub(c, token, nil); results <- err }(token)
	}
	firstErr, secondErr := <-results, <-results
	if (firstErr == nil) == (secondErr == nil) {
		t.Fatalf("expected one successful rotation, got %v and %v", firstErr, secondErr)
	}
	rotated, err = store.WorkbenchGitHubProgress(id)
	if err != nil {
		t.Fatal(err)
	}
	refs, err := credentials.List(context.Background())
	if err != nil || len(refs) != 1 || refs[0] != rotated.CredentialRef {
		t.Fatalf("concurrent rotation left incorrect references: %v %v", refs, err)
	}
	if err := s.RemoveWorkbench(id); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Get(context.Background(), rotated.CredentialRef); err == nil {
		t.Fatal("disconnected credential remains")
	}
}

func TestGitHubCredentialCleanupAfterInterruptedRotation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":1,"login":"user"}`)
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
		case "/repos/owner/repo/issues":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	credentials := &failOnceGitHubCredentials{memoryGitHubCredentials: &memoryGitHubCredentials{values: make(map[string]string)}}
	open := func() *Service {
		t.Helper()
		s, err := NewWithWorkbenchDependencies(dir, func(path string) (Store, error) { return dal.Open(path) }, BuiltinWorkbenchPluginRegistry(), ObsidianSyncOptions{MaxRetries: 3}, WorkbenchGitHubDependencies{Credentials: credentials, APIURL: server.URL, Transport: server.Client().Transport})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	id, err := s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Name: "Repo", Repository: "owner/repo"}, "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	store := s.store.(workbenchGitHubStore)
	old, err := store.WorkbenchGitHubProgress(id)
	if err != nil {
		t.Fatal(err)
	}
	// A new Keychain value may be written just before the reference transaction.
	if err := credentials.Put(context.Background(), "interrupted-before-commit", "uncommitted"); err != nil {
		t.Fatal(err)
	}
	credentials.failMu.Lock()
	credentials.failRef = old.CredentialRef
	credentials.failMu.Unlock()
	c := common.WorkbenchConnection{ID: id, Version: 1, Name: "Repo", Repository: "owner/repo"}
	if _, err := s.ConfigureWorkbenchGitHub(c, "second", nil); err != nil {
		t.Fatal(err)
	}
	current, err := store.WorkbenchGitHubProgress(id)
	if err != nil || current.CredentialRef == old.CredentialRef {
		t.Fatalf("rotation did not commit: %+v %v", current, err)
	}
	if _, err := credentials.Get(context.Background(), old.CredentialRef); err != nil {
		t.Fatal("injected deletion failure did not leave old reference")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	defer s.Close()
	for until := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		refs, err := credentials.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) == 1 && refs[0] == current.CredentialRef {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("restart did not remove interrupted rotation entries: %v", refs)
		}
	}
	if token, err := credentials.Get(context.Background(), current.CredentialRef); err != nil || token != "second" {
		t.Fatal("active credential did not survive cleanup")
	}
}

func TestGitHubConnectionSaveReportsCommittedResultWhenSchedulingFails(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":1,"login":"user"}`)
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	credentials := &memoryGitHubCredentials{values: make(map[string]string)}
	s, err := NewWithWorkbenchDependencies(t.TempDir(), func(path string) (Store, error) {
		store, err := dal.Open(path)
		if err != nil {
			return nil, err
		}
		return &failGitHubScanStore{store}, nil
	}, BuiltinWorkbenchPluginRegistry(), ObsidianSyncOptions{MaxRetries: 3}, WorkbenchGitHubDependencies{Credentials: credentials, APIURL: server.URL, Transport: server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Name: "Repo", Repository: "owner/repo"}, "secret", nil)
	if err != nil || id == "" {
		t.Fatalf("committed configuration was reported as failed: %q %v", id, err)
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil || snapshot.Repository != "owner/repo" || snapshot.AccessState != "pending" {
		t.Fatalf("saved connection unavailable: %+v %v", snapshot, err)
	}
}

func TestGitHubCleanupWaitsForInFlightCredentialSave(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":1,"login":"user"}`)
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
		case "/repos/owner/repo/issues":
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	credentials := &blockingGitHubCredentials{memoryGitHubCredentials: &memoryGitHubCredentials{values: make(map[string]string)}, stored: make(chan struct{}), release: make(chan struct{})}
	s, err := NewWithWorkbenchDependencies(t.TempDir(), func(path string) (Store, error) { return dal.Open(path) }, BuiltinWorkbenchPluginRegistry(), ObsidianSyncOptions{MaxRetries: 3}, WorkbenchGitHubDependencies{Credentials: credentials, APIURL: server.URL, Transport: server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	type result struct {
		id  string
		err error
	}
	saved := make(chan result, 1)
	go func() {
		id, err := s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Name: "Repo", Repository: "owner/repo"}, "secret", nil)
		saved <- result{id, err}
	}()
	select {
	case <-credentials.stored:
	case <-time.After(3 * time.Second):
		close(credentials.release)
		t.Fatal("credential write did not begin")
	}
	cleaned := make(chan error, 1)
	go func() { cleaned <- s.cleanupWorkbenchGitHubCredentials(context.Background(), credentials) }()
	close(credentials.release)
	outcome := <-saved
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if err := <-cleaned; err != nil {
		t.Fatal(err)
	}
	progress, err := s.store.(workbenchGitHubStore).WorkbenchGitHubProgress(outcome.id)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := credentials.Get(context.Background(), progress.CredentialRef); err != nil || value != "secret" {
		t.Fatal("cleanup removed a credential during save")
	}
}

func TestGitHubConnectionKeepsIdentityAfterRepositoryRename(t *testing.T) {
	var renamed atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":1,"login":"user"}`)
		case "/repos/owner/old":
			if renamed.Load() {
				http.Redirect(w, r, "/repositories/42", http.StatusMovedPermanently)
				return
			}
			fmt.Fprint(w, `{"id":42,"full_name":"owner/old","has_issues":true}`)
		case "/repositories/42", "/repos/owner/new":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/new","has_issues":true}`)
		case "/repos/owner/old/issues", "/repos/owner/new/issues":
			fmt.Fprint(w, `[{"id":15,"number":1,"title":"Issue","state":"open","updated_at":"2026-01-01T00:00:00Z"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	credentials := &memoryGitHubCredentials{values: make(map[string]string)}
	s, err := NewWithWorkbenchDependencies(t.TempDir(), func(path string) (Store, error) { return dal.Open(path) }, BuiltinWorkbenchPluginRegistry(), ObsidianSyncOptions{MaxRetries: 3}, WorkbenchGitHubDependencies{Credentials: credentials, APIURL: server.URL, Transport: server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Name: "Repo", Repository: "owner/old"}, "token", nil)
	if err != nil {
		t.Fatal(err)
	}
	store := s.store.(workbenchGitHubStore)
	before, err := store.WorkbenchGitHubProgress(id)
	if err != nil {
		t.Fatal(err)
	}
	var taskID string
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); time.Sleep(5 * time.Millisecond) {
		page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 1 {
			taskID = page.Items[0].ID
			break
		}
	}
	if taskID == "" {
		t.Fatal("initial issue did not sync")
	}
	if err := s.SetWorkbenchLabels(taskID, []string{"kept"}); err != nil {
		t.Fatal(err)
	}
	renamed.Store(true)
	updatedID, err := s.ConfigureWorkbenchGitHub(common.WorkbenchConnection{ID: id, Version: 1, Name: "Repo", Repository: "owner/old"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.WorkbenchGitHubProgress(id)
	if err != nil || updatedID != id || after.RepositoryID != before.RepositoryID || after.CredentialRef != before.CredentialRef {
		t.Fatalf("repository identity changed: %+v %v", after, err)
	}
	snapshot, err := s.WorkbenchSnapshot(id)
	if err != nil || snapshot.Repository != "owner/new" || snapshot.Version != 2 {
		t.Fatalf("repository rename not applied: %+v %v", snapshot, err)
	}
	var restored common.WorkbenchTask
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); time.Sleep(5 * time.Millisecond) {
		restored, err = s.WorkbenchTask(taskID)
		if err == nil && restored.NativeFields["repository"] == "owner/new" {
			break
		}
	}
	if err != nil || restored.ID != taskID || restored.NativeFields["repository"] != "owner/new" || len(restored.WorkbenchLabels) != 1 || restored.WorkbenchLabels[0].Name != "kept" {
		t.Fatalf("renamed repository lost task identity or metadata: %+v %v", restored, err)
	}
}

type gitHubSyncBetweenReadsStore struct {
	*dal.Store
	afterRead func([]common.WorkbenchSnapshot) error
}

func (s *gitHubSyncBetweenReadsStore) WorkbenchConnections() ([]common.WorkbenchSnapshot, error) {
	list, err := s.Store.WorkbenchConnections()
	if err == nil && s.afterRead != nil {
		afterRead := s.afterRead
		s.afterRead = nil
		err = afterRead(list)
	}
	return list, err
}

func TestGitHubCredentialRotationDuringSnapshotCommit(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":1,"login":"user"}`)
		case "/repos/owner/repo":
			fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	credentials := &memoryGitHubCredentials{values: map[string]string{"old-ref": "old-token"}}
	db, err := dal.Open(filepath.Join(t.TempDir(), "apilot.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &gitHubSyncBetweenReadsStore{Store: db}
	s := &Service{
		ctx: context.Background(), store: store, workbenchPlugins: BuiltinWorkbenchPluginRegistry().plugins,
		githubCredentials: credentials, githubBaseURL: server.URL, githubTransport: server.Client().Transport,
		githubRate: integrations.NewWorkbenchGitHubRate(4),
	}

	c := common.WorkbenchConnection{ID: "repo", Kind: common.WorkbenchGitHubRepository, Version: 1, Name: "Repo", Repository: "owner/repo", Paused: true, WriteEnabled: true}
	if err := store.SaveWorkbenchGitHub(c, 0, common.WorkbenchGitHubProgress{
		ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, AccountLogin: "user", CredentialRef: "old-ref", AccessState: "available",
	}); err != nil {
		t.Fatal(err)
	}
	committed := false
	store.afterRead = func(list []common.WorkbenchSnapshot) error {
		for _, row := range list {
			if row.ID == c.ID {
				now := time.Now()
				err := store.CommitWorkbenchGitHubSync(c, nil, nil, nil, now, row.SnapshotVersion, now, true)
				if err == nil {
					committed = true
				}
				return err
			}
		}
		return fmt.Errorf("connection %q missing from SQLite read", c.ID)
	}
	if _, err := s.ConfigureWorkbenchGitHub(c, "new-token", nil); err != nil {
		t.Fatalf("rotation after concurrent sync commit (committed=%t): %v", committed, err)
	}
	snapshot, err := s.WorkbenchSnapshot(c.ID)
	if !committed || err != nil || snapshot.Version != 2 || !snapshot.Paused || !snapshot.WriteEnabled {
		t.Fatalf("rotation did not preserve configuration: committed=%t version=%d paused=%t write-enabled=%t err=%v", committed, snapshot.Version, snapshot.Paused, snapshot.WriteEnabled, err)
	}
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := credentials.Get(context.Background(), progress.CredentialRef); err != nil || value != "new-token" {
		t.Fatal("rotation did not activate the replacement credential")
	}
	if _, err := credentials.Get(context.Background(), "old-ref"); err == nil {
		t.Fatal("rotation retained the superseded credential")
	}
}
