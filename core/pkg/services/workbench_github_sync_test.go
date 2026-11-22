package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func TestGitHubIncrementalAndFullReadSameChanges(t *testing.T) {
	var updated atomic.Bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issue := func(id int, title, stamp string) string {
			return fmt.Sprintf(`{"id":%d,"number":%d,"title":%q,"state":"open","updated_at":%q}`, id, id, title, stamp)
		}
		old, recent := "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"
		if r.URL.Query().Get("since") != "" {
			fmt.Fprintf(w, "[%s]", issue(2, "changed", recent))
			return
		}
		if r.URL.Query().Get("page") == "" {
			query := r.URL.Query()
			query.Set("page", "2")
			w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, query.Encode()))
			fmt.Fprintf(w, "[%s]", issue(1, "unchanged", old))
			return
		}
		if updated.Load() {
			fmt.Fprintf(w, "[%s]", issue(2, "changed", recent))
			return
		}
		fmt.Fprintf(w, "[%s]", issue(2, "original", old))
	}))
	defer server.Close()
	client, err := integrations.NewWorkbenchGitHubClient(server.URL, "test-token", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	connection := common.WorkbenchConnection{ID: "repo", Repository: "owner/repo"}
	read := func(since *time.Time) ([]common.WorkbenchTask, *integrations.WorkbenchGitHubReadStats, time.Duration) {
		t.Helper()
		stats := &integrations.WorkbenchGitHubReadStats{Started: time.Now()}
		tasks, err := client.Issues(integrations.WithWorkbenchGitHubReadStats(context.Background(), stats), connection, since, 42)
		elapsed := time.Since(stats.Started)
		if err != nil {
			t.Fatal(err)
		}
		return tasks, stats, elapsed
	}
	baseline, _, _ := read(nil)
	updated.Store(true)
	since := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	delta, incremental, incrementalTime := read(&since)
	full, complete, fullTime := read(nil)
	merged := mergeGitHubIncremental(baseline, delta)
	if incremental.Requests != 1 || incremental.Pages != 1 || complete.Requests != 2 || complete.Pages != 2 || len(merged) != len(full) {
		t.Fatalf("unexpected comparison: incremental=%+v full=%+v merged=%d full_items=%d", incremental, complete, len(merged), len(full))
	}
	for i := range full {
		if merged[i].SourceKey != full[i].SourceKey || merged[i].Title != full[i].Title {
			t.Fatalf("different result: %+v vs %+v", merged, full)
		}
	}
	t.Logf("same changes: incremental requests=%d pages=%d elapsed=%s; full requests=%d pages=%d elapsed=%s; tasks=%d", incremental.Requests, incremental.Pages, incrementalTime, complete.Requests, complete.Pages, fullTime, len(full))
}

func TestGitHubSyncRejectsChangedRepositoryIdentity(t *testing.T) {
	var issueReads atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo" {
			issueReads.Add(1)
		}
		fmt.Fprint(w, `{"id":99,"full_name":"owner/repo","has_issues":true}`)
	}))
	defer server.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
	store := s.store.(workbenchGitHubStore)
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = s.readWorkbenchGitHub(context.Background(), c, nil)
	if err == nil || issueReads.Load() != 0 {
		t.Fatalf("wrong repository was read: %v, issue reads=%d", err, issueReads.Load())
	}
}

func TestGitHubIncrementalFailureDoesNotAdvanceSnapshotOrCursor(t *testing.T) {
	var phase atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		if r.URL.Path != "/repos/owner/repo/issues" {
			http.NotFound(w, r)
			return
		}
		switch phase.Load() {
		case 0:
			if r.URL.Query().Get("since") != "" {
				t.Error("first read was not full")
			}
			fmt.Fprint(w, `[{"id":1,"number":1,"title":"one","state":"open","updated_at":"2026-01-01T00:00:00Z"},{"id":2,"number":2,"title":"two","state":"open","updated_at":"2026-01-01T00:00:00Z"}]`)
		case 1:
			if r.URL.Query().Get("page") == "2" {
				http.Error(w, "unavailable", 503)
				return
			}
			if r.URL.Query().Get("since") == "" {
				t.Error("expected incremental read")
			}
			query := r.URL.Query()
			query.Set("page", "2")
			w.Header().Set("Link", fmt.Sprintf(`<https://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, query.Encode()))
			fmt.Fprint(w, `[{"id":1,"number":1,"title":"changed","state":"open","updated_at":"2026-01-02T00:00:00Z"}]`)
		case 2:
			fmt.Fprint(w, `[{"id":1,"number":1,"title":"changed","state":"open","updated_at":"2026-01-02T00:00:00Z"},{"id":3,"number":3,"title":"three","state":"open","updated_at":"2026-01-02T00:00:00Z"}]`)
		}
	}))
	defer server.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
	store := s.store.(interface {
		workbenchGitHubStore
		workbenchStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "pending"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	run := func(want string) common.WorkbenchAttempt {
		t.Helper()
		id, err := s.RunWorkbenchSync(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
			snapshot, err := s.WorkbenchSnapshot(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].ID == id && snapshot.Attempts[0].State != "running" {
				if snapshot.Attempts[0].State != want {
					t.Fatalf("attempt: %+v", snapshot.Attempts[0])
				}
				return snapshot.Attempts[0]
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("sync did not finish")
		return common.WorkbenchAttempt{}
	}
	initial := run("success")
	if initial.SyncType != "full" || initial.PageCount != 1 || initial.RequestCount != 2 {
		t.Fatalf("initial sync metrics: %+v", initial)
	}
	before, err := store.WorkbenchObjects(c.ID)
	if err != nil || len(before) != 2 {
		t.Fatalf("baseline: %+v %v", before, err)
	}
	baseline, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || baseline.Cursor == nil {
		t.Fatalf("cursor: %+v %v", baseline, err)
	}
	phase.Store(1)
	failure := run("failed")
	if failure.SyncType != "incremental" || failure.PageCount != 2 || failure.RequestCount != 3 {
		t.Fatalf("failed sync metrics: %+v", failure)
	}
	failed, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || failed.Cursor == nil || !failed.Cursor.Equal(*baseline.Cursor) {
		t.Fatalf("failed sync advanced cursor: %+v %v", failed, err)
	}
	items, err := store.WorkbenchObjects(c.ID)
	if err != nil || len(items) != 2 || items[0].Title != before[0].Title {
		t.Fatalf("failed sync published partial tasks: %+v %v", items, err)
	}
	phase.Store(2)
	run("success")
	items, err = store.WorkbenchObjects(c.ID)
	if err != nil || len(items) != 3 {
		t.Fatalf("incremental merge: %+v %v", items, err)
	}
	for _, task := range before {
		found := false
		for _, item := range items {
			if item.ID == task.ID && item.SourceKey == task.SourceKey {
				found = true
			}
		}
		if !found {
			t.Fatalf("identity changed for %s", task.SourceKey)
		}
	}
}

func TestGitHubFullReconciliationOfMissingIssue(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		repoStatus int
		wantTasks  int
		wantError  string
	}{
		{name: "transferred", status: http.StatusMovedPermanently},
		{name: "deleted", status: http.StatusGone},
		{name: "ambiguous", status: http.StatusNotFound, wantTasks: 1},
		{name: "issue read revoked", status: http.StatusForbidden, wantError: "FORBIDDEN"},
		{name: "repository access lost after deletion response", status: http.StatusGone, repoStatus: http.StatusNotFound, wantError: "NOT_FOUND"},
		{name: "temporary failure", status: http.StatusBadGateway, wantError: "SOURCE_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var probed atomic.Bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo":
					if probed.Load() && tc.repoStatus != 0 {
						w.WriteHeader(tc.repoStatus)
						return
					}
					fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","has_issues":true}`)
				case "/repos/owner/repo/issues":
					fmt.Fprint(w, `[]`)
				case "/repos/owner/repo/issues/1":
					probed.Store(true)
					if tc.status == http.StatusMovedPermanently {
						w.Header().Set("Location", "/repos/other/repo/issues/9")
					}
					w.WriteHeader(tc.status)
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
			s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
			store := s.store.(workbenchGitHubStore)
			c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1}
			p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
			if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
				t.Fatal(err)
			}
			before := []common.WorkbenchTask{{ID: "old", SourceKey: "issue:1\x000", NativeID: "1", Title: "last confirmed"}}
			tasks, _, full, err := s.readWorkbenchGitHub(context.Background(), c, before)
			if !full || !probed.Load() || len(tasks) != tc.wantTasks {
				t.Fatalf("reconciliation: tasks=%+v full=%t error=%v", tasks, full, err)
			}
			var code string
			if err != nil {
				code = common.ProtocolError(err).Code
			}
			if code != tc.wantError {
				t.Fatalf("expected error code %q, got %q (%v)", tc.wantError, code, err)
			}
			if tc.wantTasks == 1 && (tasks[0].ID != before[0].ID || tasks[0].Title != before[0].Title) {
				t.Fatalf("last confirmed task changed: %+v", tasks[0])
			}
		})
	}
}

func TestGitHubManualSyncHonorsPollInterval(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		w.Header().Set("X-Poll-Interval", "120")
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
	store := s.store.(workbenchGitHubStore)
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.readWorkbenchGitHub(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	_, err = s.RunWorkbenchSync(c.ID)
	var problem *common.Error
	if !errors.As(err, &problem) || problem.Code != "RATE_LIMITED" {
		t.Fatalf("manual poll ignored interval: %v", err)
	}
}

func TestGitHubPollIntervalSurvivesServiceRestart(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		calls.Add(1)
		w.Header().Set("X-Poll-Interval", "120")
		fmt.Fprint(w, `[]`)
	}))
	defer server.Close()
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
	store := s.store.(workbenchGitHubStore)
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "pending"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	id, err := s.RunWorkbenchSync(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	completed := false
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		snapshot, err := s.WorkbenchSnapshot(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].ID == id && snapshot.Attempts[0].State == "success" {
			completed = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !completed {
		t.Fatal("first sync did not finish")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.RunWorkbenchSync(c.ID)
	var problem *common.Error
	if !errors.As(err, &problem) || problem.Code != "RATE_LIMITED" {
		t.Fatalf("restart lost poll interval: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("restart made %d requests, expected 1", calls.Load())
	}
}

func TestGitHubAccountBackoffSurvivesRestartAndIsShared(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
	store := s.store.(workbenchGitHubStore)
	for i, name := range []string{"one", "two"} {
		c := common.WorkbenchConnection{ID: name, Kind: common.WorkbenchGitHubRepository, Repository: "owner/" + name, Version: 1}
		p := common.WorkbenchGitHubProgress{ConnectionID: name, RepositoryID: int64(i + 1), AccountID: 1, CredentialRef: "ref", AccessState: "pending"}
		if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.RunWorkbenchSync("one")
	if err != nil {
		t.Fatal(err)
	}
	finished := false
	for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
		snapshot, err := s.WorkbenchSnapshot("one")
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].ID == id && snapshot.Attempts[0].State == "failed" {
			finished = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !finished {
		t.Fatal("rate-limited sync did not finish")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.RunWorkbenchSync("two")
	var problem *common.Error
	if !errors.As(err, &problem) || problem.Code != "RATE_LIMITED" {
		t.Fatalf("shared backoff lost after restart: %v", err)
	}
	for _, name := range []string{"one", "two"} {
		snapshot, err := s.WorkbenchSnapshot(name)
		if err != nil || snapshot.RateLimitUntil == nil || !snapshot.RateLimitUntil.After(time.Now()) {
			t.Fatalf("%s did not display shared wait: %+v %v", name, snapshot, err)
		}
	}
	if s.githubRate.BackoffDelay(2, time.Now()) != 0 {
		t.Fatal("backoff affected a different account")
	}
	if calls.Load() != 1 {
		t.Fatalf("unexpected requests: %d", calls.Load())
	}
}

func TestGitHubPauseCancelsOneRepositoryAndResumeCatchesUp(t *testing.T) {
	var revision atomic.Int32
	var block atomic.Bool
	started := make(chan struct{}, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		if r.URL.Path == "/repos/owner/one/issues" && block.Load() {
			select {
			case started <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		if r.URL.Path != "/repos/owner/one/issues" && r.URL.Path != "/repos/owner/two/issues" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `[{"id":1,"number":1,"title":"revision-%d","state":"open","updated_at":"2026-01-0%dT00:00:00Z"}]`, revision.Load(), revision.Load()+1)
	}))
	defer server.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
	store := s.store.(interface {
		workbenchGitHubStore
		workbenchStore
	})
	for i, name := range []string{"one", "two"} {
		c := common.WorkbenchConnection{ID: name, Kind: common.WorkbenchGitHubRepository, Repository: "owner/" + name, Version: 1}
		p := common.WorkbenchGitHubProgress{ConnectionID: name, RepositoryID: int64(i + 1), AccountID: 1, CredentialRef: "ref", AccessState: "pending"}
		if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(id, title string) {
		t.Helper()
		for until := time.Now().Add(4 * time.Second); time.Now().Before(until); {
			items, err := store.WorkbenchObjects(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) == 1 && items[0].Title == title {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("%s did not reach %q", id, title)
	}
	for _, id := range []string{"one", "two"} {
		if _, err := s.RunWorkbenchSync(id); err != nil {
			t.Fatal(err)
		}
		wait(id, "revision-0")
	}
	before, err := store.WorkbenchObjects("one")
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := store.WorkbenchGitHubProgress("one")
	if err != nil || baseline.Cursor == nil {
		t.Fatalf("missing baseline: %+v %v", baseline, err)
	}
	revision.Store(1)
	block.Store(true)
	if _, err := s.RunWorkbenchSync("one"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	paused, err := s.PauseWorkbench("one", 1, true)
	if err != nil || !paused.Paused {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	if _, err := s.RunWorkbenchSync("one"); err == nil {
		t.Fatal("paused repository allowed sync")
	}
	if _, err := s.RunWorkbenchSync("two"); err != nil {
		t.Fatal(err)
	}
	wait("two", "revision-1")
	items, err := store.WorkbenchObjects("one")
	if err != nil || len(items) != 1 || items[0].Title != "revision-0" {
		t.Fatalf("cancelled sync published data: %+v %v", items, err)
	}
	progress, err := store.WorkbenchGitHubProgress("one")
	if err != nil || progress.Cursor == nil || !progress.Cursor.Equal(*baseline.Cursor) {
		t.Fatalf("cancelled sync advanced cursor: %+v %v", progress, err)
	}
	block.Store(false)
	if _, err := s.PauseWorkbench("one", paused.Version, false); err != nil {
		t.Fatal(err)
	}
	wait("one", "revision-1")
	items, err = store.WorkbenchObjects("one")
	if err != nil || items[0].ID != before[0].ID {
		t.Fatalf("resume changed identity: %+v %v", items, err)
	}
}

func TestGitHubAndObsidianMetadataSurviveSyncAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := s.store.(interface {
		workbenchGitHubStore
		workbenchGitHubSyncStore
		workbenchStore
	})
	now := time.Now().UTC()
	ids := make([]string, 0, 3)
	for i, name := range []string{"one", "two"} {
		c := common.WorkbenchConnection{ID: name, Kind: common.WorkbenchGitHubRepository, Repository: "owner/" + name, Version: 1, Paused: true}
		p := common.WorkbenchGitHubProgress{ConnectionID: name, RepositoryID: int64(i + 1), AccountID: 1, CredentialRef: "ref", AccessState: "available"}
		if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
			t.Fatal(err)
		}
		tasks := []common.WorkbenchTask{{SourceKey: fmt.Sprintf("issue:%d", i+1), NativeID: "7", Title: name, State: "open", NativeFields: map[string]string{"issue_id": fmt.Sprint(i + 1), "repository": c.Repository, "updated_at": "2026-01-01T00:00:00Z"}}}
		matchWorkbench(c, nil, tasks)
		if err := store.CommitWorkbenchGitHubSync(c, nil, nil, tasks, now, 0, now, true); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, tasks[0].ID)
		if err := s.SetWorkbenchLabels(tasks[0].ID, []string{"local-" + name}); err != nil {
			t.Fatal(err)
		}
		if err := s.PutWorkbenchAnnotation(tasks[0].ID, "note-"+name, "annotation-"+name); err != nil {
			t.Fatal(err)
		}
		updated := []common.WorkbenchTask{{SourceKey: fmt.Sprintf("issue:%d", i+1), NativeID: "7", Title: name + " updated", State: "open", NativeFields: map[string]string{"issue_id": fmt.Sprint(i + 1), "repository": c.Repository, "updated_at": "2026-01-02T00:00:00Z"}}}
		merged := mergeGitHubIncremental(tasks, updated)
		matchWorkbench(c, tasks, merged)
		if err := store.CommitWorkbenchGitHubSync(c, nil, tasks, merged, now.Add(time.Second), 1, now.Add(time.Second), false); err != nil {
			t.Fatal(err)
		}
	}
	vault := common.WorkbenchConnection{ID: "vault", Kind: common.WorkbenchObsidianTasks, Version: 1, Paused: true}
	if err := store.SaveWorkbench(vault, false); err != nil {
		t.Fatal(err)
	}
	notes := []common.WorkbenchTask{{SourceKey: "note:1", Title: "Obsidian", State: "open"}}
	matchWorkbench(vault, nil, notes)
	if err := store.RecordWorkbench(vault, nil, nil, notes, &now); err != nil {
		t.Fatal(err)
	}
	ids = append(ids, notes[0].ID)
	if err := s.SetWorkbenchLabels(notes[0].ID, []string{"local-vault"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutWorkbenchAnnotation(notes[0].ID, "note-vault", "annotation-vault"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = openService(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i, name := range []string{"one", "two", "vault"} {
		task, err := s.WorkbenchTask(ids[i])
		if err != nil || len(task.WorkbenchLabels) != 1 || task.WorkbenchLabels[0].Name != "local-"+name {
			t.Fatalf("%s labels: %+v %v", name, task, err)
		}
		annotations, err := s.WorkbenchAnnotations(ids[i])
		if err != nil || len(annotations) != 1 || annotations[0].Body != "annotation-"+name {
			t.Fatalf("%s annotations: %+v %v", name, annotations, err)
		}
		if i < 2 && (task.NativeID != "7" || task.Title != name+" updated") {
			t.Fatalf("%s task identity changed: %+v", name, task)
		}
	}
}

func TestGitHubIncrementalReorderingAndFullReconciliation(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	var clock atomic.Int64
	clock.Store(base.Add(-15 * time.Minute).Unix())
	var phase atomic.Int32
	stamp := func(at time.Time) string { return at.UTC().Format(time.RFC3339) }
	issue := func(id int, title string, at time.Time) string {
		return fmt.Sprintf(`{"id":%d,"number":%d,"title":%q,"state":"open","updated_at":%q}`, id, id, title, stamp(at))
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		if r.URL.Path == "/repos/owner/repo/issues/5" && phase.Load() == 3 {
			w.WriteHeader(http.StatusGone)
			return
		}
		if r.URL.Path != "/repos/owner/repo/issues" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		switch phase.Load() {
		case 0:
			if q.Get("since") != "" || q.Get("sort") != "created" {
				t.Error("baseline was not a full read")
			}
			fmt.Fprintf(w, "[%s,%s,%s,%s,%s]", issue(1, "old-1", base.Add(-30*time.Minute)), issue(2, "old-2", base.Add(-30*time.Minute)), issue(3, "old-3", base.Add(-30*time.Minute)), issue(4, "old-4", base.Add(-30*time.Minute)), issue(5, "old-5", base.Add(-30*time.Minute)))
		case 1:
			since, err := time.Parse(time.RFC3339, q.Get("since"))
			if err != nil || since.After(base.Add(-10*time.Minute)) || q.Get("sort") != "updated" {
				t.Errorf("first incremental window: %q", q.Get("since"))
			}
			if q.Get("page") == "" {
				next := *r.URL
				values := next.Query()
				values.Set("page", "2")
				next.RawQuery = values.Encode()
				w.Header().Set("Link", fmt.Sprintf(`<https://%s%s>; rel="next"`, r.Host, next.String()))
				fmt.Fprintf(w, "[%s]", issue(1, "new-1", base.Add(-3*time.Minute)))
			} else {
				fmt.Fprintf(w, "[%s]", issue(3, "new-3", base.Add(-time.Minute)))
			}
		case 2:
			since, err := time.Parse(time.RFC3339, q.Get("since"))
			if err != nil || since.After(base.Add(-2*time.Minute)) || !since.After(base.Add(-10*time.Minute)) {
				t.Errorf("overlap window: %q", q.Get("since"))
			}
			fmt.Fprintf(w, "[%s]", issue(2, "new-2", base.Add(-2*time.Minute)))
		case 3:
			if q.Get("since") != "" || q.Get("sort") != "created" {
				t.Error("periodic reconciliation was not full")
			}
			fmt.Fprintf(w, "[%s,%s,%s,%s]", issue(1, "new-1", base.Add(-3*time.Minute)), issue(2, "new-2", base.Add(-2*time.Minute)), issue(3, "new-3", base.Add(-time.Minute)), issue(4, "new-4", base.Add(-10*time.Minute)))
		}
	}))
	defer server.Close()
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.githubCredentials, s.githubBaseURL, s.githubTransport = githubTestCredentials{}, server.URL, server.Client().Transport
	s.githubNow = func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	store := s.store.(interface {
		workbenchGitHubStore
		workbenchStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "pending"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	run := func() common.WorkbenchAttempt {
		t.Helper()
		id, err := s.RunWorkbenchSync(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
			snapshot, err := s.WorkbenchSnapshot(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Attempts) > 0 && snapshot.Attempts[0].ID == id && snapshot.Attempts[0].State != "running" {
				if snapshot.Attempts[0].State != "success" {
					t.Fatalf("sync failed: %+v", snapshot.Attempts[0])
				}
				return snapshot.Attempts[0]
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatal("sync timeout")
		return common.WorkbenchAttempt{}
	}
	lookup := func() map[string]common.WorkbenchTask {
		t.Helper()
		items, err := store.WorkbenchObjects(c.ID)
		if err != nil {
			t.Fatal(err)
		}
		result := make(map[string]common.WorkbenchTask)
		for _, item := range items {
			result[item.NativeID] = item
		}
		return result
	}
	run()
	baseline := lookup()
	if len(baseline) != 5 {
		t.Fatalf("baseline: %+v", baseline)
	}
	if err := s.SetWorkbenchLabels(baseline["2"].ID, []string{"retained"}); err != nil {
		t.Fatal(err)
	}
	clock.Store(base.Unix())
	phase.Store(1)
	first := run()
	if first.SyncType != "incremental" || first.PageCount != 2 || first.RequestCount != 3 {
		t.Fatalf("first incremental metrics: %+v", first)
	}
	items := lookup()
	if items["1"].Title != "new-1" || items["2"].Title != "old-2" || items["4"].Title != "old-4" {
		t.Fatalf("unexpected reordered snapshot: %+v", items)
	}
	clock.Store(base.Add(time.Minute).Unix())
	phase.Store(2)
	second := run()
	items = lookup()
	if second.PageCount != 1 || second.RequestCount != 2 || items["5"].ID != baseline["5"].ID || items["2"].Title != "new-2" || items["4"].Title != "old-4" {
		t.Fatalf("overlap did not recover recent item: %+v", items)
	}
	clock.Store(base.Add(25 * time.Hour).Unix())
	phase.Store(3)
	full := run()
	items = lookup()
	if full.SyncType != "full" || items["4"].Title != "new-4" {
		t.Fatalf("full reconciliation did not recover older item: %+v", items)
	}
	if _, exists := items["5"]; exists {
		t.Fatal("confirmed deleted issue survived full reconciliation")
	}
	for id, before := range baseline {
		if id == "5" {
			continue
		}
		if items[id].ID != before.ID {
			t.Fatalf("identity changed for %s", id)
		}
	}
	verified, err := s.WorkbenchTask(baseline["2"].ID)
	if err != nil || len(verified.WorkbenchLabels) != 1 || verified.WorkbenchLabels[0].Name != "retained" {
		t.Fatalf("metadata changed: %+v %v", verified, err)
	}
}

func TestMergeGitHubIncrementalPreservesUnseenAndNewerTasks(t *testing.T) {
	before := []common.WorkbenchTask{
		{SourceKey: "issue:1", Title: "newer", NativeFields: map[string]string{"updated_at": "2026-01-02T00:00:00Z"}},
		{SourceKey: "issue:2", Title: "unseen"},
	}
	observed := []common.WorkbenchTask{
		{SourceKey: "issue:1", Title: "older", NativeFields: map[string]string{"updated_at": "2026-01-01T00:00:00Z"}},
		{SourceKey: "issue:3", Title: "added", NativeFields: map[string]string{"updated_at": "2026-01-03T00:00:00Z"}},
	}
	got := mergeGitHubIncremental(before, observed)
	if len(got) != 3 || got[0].Title != "newer" || got[1].Title != "unseen" || got[2].Title != "added" {
		t.Fatalf("unexpected merged tasks: %+v", got)
	}
}

func TestMergeGitHubIncrementalRetainsTaskIdentity(t *testing.T) {
	connection := common.WorkbenchConnection{ID: "connection", Kind: common.WorkbenchGitHubIssues}
	before := []common.WorkbenchTask{{SourceKey: "issue:1"}, {SourceKey: "issue:2"}}
	matchWorkbench(connection, nil, before)
	merged := mergeGitHubIncremental(before, []common.WorkbenchTask{{SourceKey: "issue:1"}})
	matchWorkbench(connection, before, merged)
	for i := range before {
		if merged[i].ID != before[i].ID || merged[i].SourceKey != before[i].SourceKey {
			t.Fatalf("task %d identity changed: before=%+v after=%+v", i, before[i], merged[i])
		}
	}
}
