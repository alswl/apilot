package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/dal"
)

type githubTestCredentials struct{}

func (githubTestCredentials) Put(context.Context, string, string) error   { return nil }
func (githubTestCredentials) Get(context.Context, string) (string, error) { return "test-token", nil }
func (githubTestCredentials) Delete(context.Context, string) error        { return nil }
func (githubTestCredentials) List(context.Context) ([]string, error)      { return nil, nil }

func respondGitHubOperationRepository(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	ids := map[string]int{"/repos/owner/repo": 42, "/repos/owner/one": 1, "/repos/owner/two": 2, "/repos/owner/accepted": 1, "/repos/owner/unknown": 2, "/repos/owner/known": 3}
	id, ok := ids[r.URL.Path]
	if !ok {
		return false
	}
	fmt.Fprintf(w, `{"id":%d,"full_name":%q,"has_issues":true}`, id, r.URL.Path[len("/repos/"):])
	return true
}

func TestGitHubCreateReadbackAndUnknownOutcome(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost-response-%t", lost), func(t *testing.T) {
			var writes atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if respondGitHubOperationRepository(w, r) {
					return
				}
				if r.Header.Get("Authorization") != "Bearer test-token" {
					t.Error("missing authorization")
				}
				if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues" {
					writes.Add(1)
					if lost {
						http.Error(w, "response lost", http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusCreated)
				} else if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/repo/issues/7" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
					return
				}
				fmt.Fprint(w, `{"id":123,"number":7,"title":"created","body":"details","state":"open","updated_at":"2026-01-01T00:00:00Z"}`)
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
				workbenchTaskOperationStore
				RecoverWorkbenchTaskOperations() error
			})
			c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1, WriteEnabled: true}
			progress := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
			if err := store.SaveWorkbenchGitHub(c, 0, progress); err != nil {
				t.Fatal(err)
			}
			input := common.WorkbenchTaskWrite{RequestID: "create", ConnectionID: c.ID, ConnectionVersion: 1, Title: "created", Body: "details"}
			if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, workbenchFingerprint(input)); err != nil {
				t.Fatal(err)
			}
			receipt, err := s.executeGitHubTaskOperation(context.Background(), input.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			if lost {
				if receipt.SyncState != common.WorkbenchOperationNeedsReview {
					t.Fatalf("lost response state: %+v", receipt)
				}
				if err := store.RecoverWorkbenchTaskOperations(); err != nil {
					t.Fatal(err)
				}
				if _, err := s.reconcileGitHubTaskOperation(context.Background(), input.RequestID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.executeGitHubTaskOperation(context.Background(), input.RequestID); err != nil {
					t.Fatal(err)
				}
			} else {
				if receipt.SyncState != common.WorkbenchOperationConfirmed || receipt.SourceObjectID == "" {
					t.Fatalf("not confirmed: %+v", receipt)
				}
				page, err := s.WorkbenchTasks(common.WorkbenchQuery{Limit: 10})
				if err != nil || len(page.Items) != 1 || page.Items[0].ID != receipt.SourceObjectID || page.Items[0].Summary != "details" {
					t.Fatalf("missing readback: %+v %v", page, err)
				}
			}
			if writes.Load() != 1 {
				t.Fatalf("sent %d create requests", writes.Load())
			}
		})
	}
}

func TestGitHubReceiptHiddenAfterAccountSwitch(t *testing.T) {
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store := s.store.(interface {
		workbenchGitHubStore
		workbenchGitHubSyncStore
		workbenchTaskOperationStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Version: 1, WriteEnabled: true}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "old", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "old-request", ConnectionID: c.ID, ConnectionVersion: 1, Title: "private"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, workbenchFingerprint(input)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkbenchTaskOperationReceipt(input.RequestID); err != nil {
		t.Fatal(err)
	}
	c.Version = 2
	p.AccountID, p.CredentialRef = 2, "new"
	if err := store.SaveWorkbenchGitHub(c, 1, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkbenchTaskOperationReceipt(input.RequestID); err == nil {
		t.Fatal("receipt visible during account change")
	}
	now := time.Now().UTC()
	if err := store.CommitWorkbenchGitHubSync(c, nil, nil, nil, now, 1, now, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkbenchTaskOperationReceipt(input.RequestID); err == nil {
		t.Fatal("old receipt visible to new account")
	}
	if _, err := s.SubmitWorkbenchTaskOperation(input); err == nil {
		t.Fatal("replay exposed old receipt")
	}
}

func TestGitHubWriteRejectsChangedRepositoryIdentity(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
			http.Error(w, "unexpected write", http.StatusInternalServerError)
			return
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
	store := s.store.(interface {
		workbenchGitHubStore
		workbenchTaskOperationStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1, WriteEnabled: true}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "changed-repo", ConnectionID: c.ID, ConnectionVersion: 1, Title: "new issue"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, workbenchFingerprint(input)); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeGitHubTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != common.WorkbenchOperationConflict || writes.Load() != 0 {
		t.Fatalf("wrong target was not rejected: %+v, writes=%d, err=%v", receipt, writes.Load(), err)
	}
}

func TestGitHubWriteDenialDisablesWritesButRetainsReadAccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected request %s", r.Method)
		}
		w.WriteHeader(http.StatusForbidden)
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
		workbenchTaskOperationStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1, WriteEnabled: true}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "denied", ConnectionID: c.ID, ConnectionVersion: 1, Title: "new issue"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, workbenchFingerprint(input)); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeGitHubTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != common.WorkbenchOperationNotExecuted {
		t.Fatalf("denied write: %+v %v", receipt, err)
	}
	snapshot, err := s.WorkbenchSnapshot(c.ID)
	if err != nil || !snapshot.WriteDenied || !snapshot.Capabilities.Read || snapshot.Capabilities.Create || snapshot.Capabilities.SetState {
		t.Fatalf("incorrect capabilities: %+v %v", snapshot, err)
	}
	input.RequestID = "second"
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, workbenchFingerprint(input)); err == nil {
		t.Fatal("admitted write after permission denial")
	}
	c.Version = 2
	if err := store.SaveWorkbenchGitHub(c, 1, p); err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.WorkbenchSnapshot(c.ID)
	if err != nil || snapshot.WriteDenied || !snapshot.Capabilities.Create {
		t.Fatalf("revalidated configuration did not restore capability: %+v %v", snapshot, err)
	}
}

func TestGitHubUnauthorizedWriteInvalidatesAccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
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
		workbenchTaskOperationStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1, WriteEnabled: true}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "unauthorized", ConnectionID: c.ID, ConnectionVersion: 1, Title: "new issue"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, workbenchFingerprint(input)); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeGitHubTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != common.WorkbenchOperationNotExecuted {
		t.Fatalf("unauthorized write: %+v %v", receipt, err)
	}
	progress, err := store.WorkbenchGitHubProgress(c.ID)
	if err != nil || progress.AccessState != "unavailable" || progress.Cursor != nil || progress.LastFullSync != nil {
		t.Fatalf("authorization not invalidated: %+v %v", progress, err)
	}
}

func TestGitHubSecondaryLimitDoesNotDisableWritePermission(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"You have exceeded a secondary rate limit."}`)
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
		workbenchTaskOperationStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1, WriteEnabled: true}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "available"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	input := common.WorkbenchTaskWrite{RequestID: "limited", ConnectionID: c.ID, ConnectionVersion: 1, Title: "new issue"}
	if _, _, err := store.AdmitWorkbenchTaskOperation(input, c.ID, workbenchFingerprint(input)); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.executeGitHubTaskOperation(context.Background(), input.RequestID)
	if err != nil || receipt.SyncState != common.WorkbenchOperationNotExecuted || receipt.Reason != "GitHub API rate limit reached" {
		t.Fatalf("rate limit: %+v %v", receipt, err)
	}
	snapshot, err := s.WorkbenchSnapshot(c.ID)
	if err != nil || snapshot.WriteDenied || !snapshot.Capabilities.Create {
		t.Fatalf("rate limit disabled writes: %+v %v", snapshot, err)
	}
}

func TestGitHubCloseReopenAndPreflightConflict(t *testing.T) {
	var mu sync.Mutex
	state, reason, updated := "open", "", "2026-01-01T00:00:00Z"
	var writes int
	wrongWriteID := false
	lostWrite := false
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/repos/owner/repo/issues" && r.Method == http.MethodGet {
			fmt.Fprintf(w, `[{"id":123,"number":7,"title":"target","state":%q,"state_reason":%q,"updated_at":%q},{"id":124,"number":8,"title":"other","state":"open","updated_at":"2026-01-01T00:00:00Z"}]`, state, reason, updated)
			return
		}
		if r.URL.Path != "/repos/owner/repo/issues/7" {
			t.Errorf("unexpected target: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPatch {
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			writes++
			state, reason = body["state"], body["state_reason"]
			if state == "closed" && (reason != "completed" || len(body) != 2) {
				t.Errorf("invalid close payload: %v", body)
			}
			if state == "open" && len(body) != 1 {
				t.Errorf("invalid reopen payload: %v", body)
			}
			updated = fmt.Sprintf("2026-01-0%dT00:00:00Z", writes+1)
			if lostWrite {
				http.Error(w, "response lost", http.StatusInternalServerError)
				return
			}
		} else if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		issueID := 123
		if r.Method == http.MethodPatch && wrongWriteID {
			issueID = 999
		}
		fmt.Fprintf(w, `{"id":%d,"number":7,"title":"target","state":%q,"state_reason":%q,"updated_at":%q}`, issueID, state, reason, updated)
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
		workbenchGitHubSyncStore
	})
	c := common.WorkbenchConnection{ID: "github", Kind: common.WorkbenchGitHubRepository, Repository: "owner/repo", Version: 1, WriteEnabled: true}
	p := common.WorkbenchGitHubProgress{ConnectionID: c.ID, RepositoryID: 42, AccountID: 1, CredentialRef: "ref", AccessState: "pending"}
	if err := store.SaveWorkbenchGitHub(c, 0, p); err != nil {
		t.Fatal(err)
	}
	tasks, started, full, err := s.readWorkbenchGitHub(context.Background(), c, nil)
	if err != nil || !full || len(tasks) != 2 {
		t.Fatalf("initial read: %+v %v", tasks, err)
	}
	matchWorkbench(c, nil, tasks)
	if err := store.CommitWorkbenchGitHubSync(c, nil, nil, tasks, started, 0, started, true); err != nil {
		t.Fatal(err)
	}
	targetID, otherID := tasks[0].ID, tasks[1].ID
	operate := func(id, desired string) common.WorkbenchTaskReceipt {
		t.Helper()
		task, err := s.WorkbenchTask(targetID)
		if err != nil {
			t.Fatal(err)
		}
		input := common.WorkbenchTaskWrite{RequestID: id, SourceObjectID: task.ID, ConnectionVersion: task.ConnectionVersion, TargetVersion: task.TargetVersion, State: desired}
		if _, _, err := s.admitWorkbenchTaskOperation(input); err != nil {
			t.Fatal(err)
		}
		receipt, err := s.executeGitHubTaskOperation(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	for _, step := range []struct{ id, desired string }{{"close", "done"}, {"reopen", "open"}} {
		receipt := operate(step.id, step.desired)
		if receipt.SyncState != common.WorkbenchOperationConfirmed || receipt.SourceObjectID != targetID {
			t.Fatalf("%s not confirmed: %+v", step.id, receipt)
		}
		task, err := s.WorkbenchTask(targetID)
		if err != nil || task.State != step.desired {
			t.Fatalf("%s readback: %+v %v", step.id, task, err)
		}
	}
	other, err := s.WorkbenchTask(otherID)
	if err != nil || other.State != "open" || other.Title != "other" {
		t.Fatalf("other issue changed: %+v %v", other, err)
	}
	mu.Lock()
	updated = "2026-01-09T00:00:00Z"
	mu.Unlock()
	if receipt := operate("conflict", "done"); receipt.SyncState != common.WorkbenchOperationConflict {
		t.Fatalf("external change not detected: %+v", receipt)
	}
	mu.Lock()
	updated, wrongWriteID = "2026-01-03T00:00:00Z", true
	mu.Unlock()
	if receipt := operate("wrong-identity", "done"); receipt.SyncState != common.WorkbenchOperationNeedsReview {
		t.Fatalf("write to a different issue was confirmed: %+v", receipt)
	}
	mu.Lock()
	state, reason, updated, wrongWriteID, lostWrite = "open", "", "2026-01-03T00:00:00Z", false, true
	mu.Unlock()
	if receipt := operate("lost-patch", "done"); receipt.SyncState != common.WorkbenchOperationNeedsReview {
		t.Fatalf("lost response should need reconciliation: %+v", receipt)
	}
	reconciled, err := s.reconcileGitHubTaskOperation(context.Background(), "lost-patch")
	if err != nil || reconciled.SyncState != common.WorkbenchOperationConfirmed || reconciled.SourceObjectID != targetID {
		t.Fatalf("lost PATCH response was not reconciled by issue identity: %+v %v", reconciled, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if writes != 4 {
		t.Fatalf("expected four writes without retries, got %d", writes)
	}
}

func TestGitHubOperationsRecoverAcrossServiceRestart(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if respondGitHubOperationRepository(w, r) {
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/accepted/issues" {
			writes.Add(1)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":101,"number":10,"title":"accepted","state":"open","updated_at":"2026-01-01T00:00:00Z"}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/accepted/issues/10" {
			fmt.Fprint(w, `{"id":101,"number":10,"title":"accepted","state":"open","updated_at":"2026-01-01T00:00:00Z"}`)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/known/issues/30" {
			fmt.Fprint(w, `{"id":303,"number":30,"title":"known","state":"open","updated_at":"2026-01-01T00:00:00Z"}`)
			return
		}
		t.Errorf("unexpected source request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()
	dir := t.TempDir()
	db, err := dal.Open(filepath.Join(dir, "apilot.db"))
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"accepted", "unknown", "known"} {
		c := common.WorkbenchConnection{ID: name, Kind: common.WorkbenchGitHubRepository, Name: name, Repository: "owner/" + name, Version: 1, WriteEnabled: true}
		p := common.WorkbenchGitHubProgress{ConnectionID: name, RepositoryID: int64(i + 1), AccountID: 1, CredentialRef: "ref", AccessState: "available"}
		if err := db.SaveWorkbenchGitHub(c, 0, p); err != nil {
			t.Fatal(err)
		}
		if err := db.RecordWorkbenchGitHubPoll(c, 1, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		input := common.WorkbenchTaskWrite{RequestID: name, ConnectionID: name, ConnectionVersion: 1, Title: name}
		operation, _, err := db.AdmitWorkbenchTaskOperation(input, name, workbenchFingerprint(input))
		if err != nil {
			t.Fatal(err)
		}
		if name == "accepted" {
			continue
		}
		if _, err := db.ClaimWorkbenchTaskOperation(name); err != nil {
			t.Fatal(err)
		}
		if name == "known" {
			if err := db.RecordWorkbenchGitHubEvidence(name, 303, 30); err != nil {
				t.Fatal(err)
			}
			operation.Receipt.SyncState = common.WorkbenchOperationNeedsReview
			if err := db.AdvanceWorkbenchTaskOperation(name, common.WorkbenchOperationDispatching, common.WorkbenchOperationNeedsReview, operation.Receipt); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := NewWithWorkbenchDependencies(dir, func(path string) (Store, error) { return dal.Open(path) }, BuiltinWorkbenchPluginRegistry(), ObsidianSyncOptions{MaxRetries: 3}, WorkbenchGitHubDependencies{Credentials: githubTestCredentials{}, APIURL: server.URL, Transport: server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	store := s.store.(workbenchTaskOperationStore)
	for until := time.Now().Add(5 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		a, errA := store.WorkbenchTaskOperation("accepted")
		b, errB := store.WorkbenchTaskOperation("unknown")
		c, errC := store.WorkbenchTaskOperation("known")
		if errA != nil || errB != nil || errC != nil {
			t.Fatalf("read recovery: %v %v %v", errA, errB, errC)
		}
		if a.Phase == common.WorkbenchOperationConfirmed && b.Phase == common.WorkbenchOperationNeedsReview && c.Phase == common.WorkbenchOperationConfirmed {
			for _, op := range []common.WorkbenchTaskOperation{a, c} {
				if op.Receipt.SourceObjectID == "" {
					t.Fatalf("confirmed operation missing target: %+v", op.Receipt)
				}
				task, err := s.WorkbenchTask(op.Receipt.SourceObjectID)
				if err != nil || task.ConnectionID != op.Receipt.ConnectionID {
					t.Fatalf("confirmed operation linked to wrong task: %+v %v", task, err)
				}
			}
			break
		}
		if time.Now().After(until) {
			t.Fatalf("recovery did not settle: accepted=%s unknown=%s known=%s", a.Phase, b.Phase, c.Phase)
		}
	}
	input := common.WorkbenchTaskWrite{RequestID: "unknown", ConnectionID: "unknown", ConnectionVersion: 1, Title: "unknown"}
	receipt, err := s.SubmitWorkbenchTaskOperation(input)
	if err != nil || receipt.SyncState != common.WorkbenchOperationNeedsReview {
		t.Fatalf("unknown replay: %+v %v", receipt, err)
	}
	if writes.Load() != 1 {
		t.Fatalf("recovery issued %d writes, expected only the accepted request", writes.Load())
	}
}
