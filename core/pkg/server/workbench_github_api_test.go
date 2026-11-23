//go:build darwin || linux

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/config"
	"github.com/alswl/apilot/core/pkg/dal"
	"github.com/alswl/apilot/core/pkg/services"
)

type apiGitHubCredentials struct{ items sync.Map }

func (c *apiGitHubCredentials) Put(_ context.Context, ref, value string) error {
	c.items.Store(ref, value)
	return nil
}
func (c *apiGitHubCredentials) Get(_ context.Context, ref string) (string, error) {
	if value, ok := c.items.Load(ref); ok {
		return value.(string), nil
	}
	return "", common.Err("NOT_FOUND", "Credential not found")
}
func (c *apiGitHubCredentials) Delete(_ context.Context, ref string) error {
	c.items.Delete(ref)
	return nil
}
func (c *apiGitHubCredentials) List(_ context.Context) ([]string, error) {
	var refs []string
	c.items.Range(func(ref, _ any) bool { refs = append(refs, ref.(string)); return true })
	return refs, nil
}

func TestWorkbenchHTTPGitHubRepositoryLifecycle(t *testing.T) {
	const deviceCode = "fixture-device-token"
	const accessToken = "fixture-access-token"
	type issue struct {
		ID      int    `json:"id"`
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		State   string `json:"state"`
		Reason  string `json:"state_reason"`
		Updated string `json:"updated_at"`
	}
	var mu sync.Mutex
	var first issue
	second := issue{ID: 200, Number: 7, Title: "Second repo", State: "open", Updated: "2026-01-01T00:00:00Z"}
	writes := 0
	firstAllowed := true
	accountID := 17
	fixture := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/login/device/code" {
			fmt.Fprintf(w, `{"device_code":%q,"user_code":"ABCD-1234","verification_uri":%q,"expires_in":60,"interval":1}`, deviceCode, "https://"+r.Host+"/login/device")
			return
		}
		if r.URL.Path == "/login/oauth/access_token" {
			if r.FormValue("device_code") != deviceCode {
				t.Error("wrong device code")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			fmt.Fprintf(w, `{"access_token":%q,"token_type":"bearer"}`, accessToken)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			t.Error("expected app user credential")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch r.Method + " " + r.URL.Path {
		case "GET /user":
			fmt.Fprintf(w, `{"id":%d,"login":"account-%d"}`, accountID, accountID)
		case "GET /user/installations":
			fmt.Fprintf(w, `{"installations":[{"id":1,"app_slug":%q}]}`, services.GitHubAppSlug)
		case "GET /user/installations/1/repositories":
			fmt.Fprint(w, `{"repositories":[{"id":2,"full_name":"owner/second","has_issues":true}`)
			if firstAllowed {
				fmt.Fprint(w, `,{"id":1,"full_name":"owner/first","has_issues":true}`)
			}
			fmt.Fprint(w, `]}`)
		case "GET /repos/owner/first":
			fmt.Fprint(w, `{"id":1,"full_name":"owner/first","has_issues":true}`)
		case "GET /repos/owner/second":
			fmt.Fprint(w, `{"id":2,"full_name":"owner/second","has_issues":true}`)
		case "GET /repos/owner/first/issues":
			items := []issue{}
			if first.ID != 0 {
				items = append(items, first)
			}
			_ = json.NewEncoder(w).Encode(items)
		case "GET /repos/owner/second/issues":
			_ = json.NewEncoder(w).Encode([]issue{second})
		case "POST /repos/owner/first/issues", "PATCH /repos/owner/first/issues/7":
			var input issue
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writes++
			if r.Method == http.MethodPost {
				first = issue{ID: 100, Number: 7, Title: input.Title, Body: input.Body, State: "open"}
				w.WriteHeader(http.StatusCreated)
			} else {
				first.State, first.Reason = input.State, input.Reason
			}
			first.Updated = time.Now().UTC().Add(time.Duration(writes) * time.Second).Format(time.RFC3339)
			_ = json.NewEncoder(w).Encode(first)
		case "GET /repos/owner/first/issues/7":
			_ = json.NewEncoder(w).Encode(first)
		default:
			t.Errorf("unexpected GitHub request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	fixture.StartTLS()
	defer fixture.Close()

	dir := t.TempDir()
	s, err := services.NewWithWorkbenchDependencies(dir, func(path string) (services.Store, error) {
		return dal.Open(path)
	}, services.BuiltinWorkbenchPluginRegistry(), services.ObsidianSyncOptions{}, services.WorkbenchGitHubDependencies{
		Credentials: &apiGitHubCredentials{}, APIURL: fixture.URL, AuthURL: fixture.URL, Transport: fixture.Client().Transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	httpServer, err := listenWorkbenchHTTP(s, config.HTTPSocketPath(dir), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer httpServer.close()
	remote := services.NewRemote(config.SocketPath(dir))
	wait := func(description string, ready func() bool) {
		t.Helper()
		for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			if ready() {
				return
			}
		}
		t.Fatalf("timed out waiting for %s", description)
	}
	status, err := remote.StartGitHubAuthorization()
	if err != nil || status.State != "waiting" || status.UserCode != "ABCD-1234" {
		t.Fatalf("start authorization: %+v, %v", status, err)
	}
	authID := status.ID
	wait("GitHub authorization", func() bool {
		status, err := remote.GitHubAuthorizationStatus(authID)
		return err == nil && status.State == "authorized" && status.UserCode == ""
	})
	repositories, err := remote.ListGitHubRepositories(authID)
	if err != nil || !slices.Equal(repositories, []string{"owner/first", "owner/second"}) {
		t.Fatalf("discover repositories: %v, %v", repositories, err)
	}
	firstConn, err := remote.ConfigureWorkbenchGitHubAuthorized(common.WorkbenchConnection{Kind: common.WorkbenchGitHubRepository, Name: "First", Repository: "owner/first"}, authID, true)
	if err != nil {
		t.Fatal(err)
	}
	secondConn, err := remote.ConfigureWorkbenchGitHub(common.WorkbenchConnection{Kind: common.WorkbenchGitHubRepository, Name: "Second", Repository: "owner/second"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	waitSync := func(id, kind string) {
		t.Helper()
		wait(kind+" sync", func() bool {
			snapshot, err := remote.WorkbenchSyncDetails(id)
			return err == nil && len(snapshot.Attempts) > 0 && snapshot.Attempts[0].State == "success" && snapshot.Attempts[0].SyncType == kind
		})
	}
	waitSync(firstConn.ID, "full")
	waitSync(secondConn.ID, "full")
	if err := remote.SyncWorkbench(firstConn.ID); err != nil {
		t.Fatal(err)
	}
	waitSync(firstConn.ID, "incremental")

	submit := func(input common.WorkbenchTaskWrite, wantState string) common.WorkbenchTask {
		t.Helper()
		if _, err := remote.SubmitWorkbenchTaskOperation(input); err != nil {
			t.Fatal(err)
		}
		var receipt common.WorkbenchTaskReceipt
		wait(input.RequestID+" confirmation", func() bool {
			var err error
			receipt, err = remote.WorkbenchTaskOperationReceipt(input.RequestID)
			return err == nil && receipt.SyncState == "confirmed"
		})
		task, err := remote.WorkbenchTask(receipt.SourceObjectID)
		if err != nil || task.ConnectionID != firstConn.ID || task.NativeID != "7" || task.State != wantState {
			t.Fatalf("readback: %+v, %v", task, err)
		}
		return task
	}
	create := common.WorkbenchTaskWrite{RequestID: "create", ConnectionID: firstConn.ID, ConnectionVersion: firstConn.Version, Title: "Test Issue", Body: "API write fixture"}
	created := submit(create, "open")
	if created.Summary != create.Body {
		t.Fatalf("create body: %q", created.Summary)
	}
	if replay, err := remote.SubmitWorkbenchTaskOperation(create); err != nil || replay.SourceObjectID != created.ID || replay.SyncState != "confirmed" {
		t.Fatalf("create replay: %+v, %v", replay, err)
	}
	change := common.WorkbenchTaskWrite{RequestID: "complete", ConnectionID: firstConn.ID, ConnectionVersion: firstConn.Version, SourceObjectID: created.ID, TargetVersion: created.TargetVersion, State: "done"}
	closed := submit(change, "done")
	change.RequestID, change.State, change.TargetVersion = "reopen", "open", closed.TargetVersion
	if reopened := submit(change, "open"); reopened.ID != created.ID {
		t.Fatalf("issue identity changed: %s", reopened.ID)
	}
	mu.Lock()
	writeCount := writes
	firstAllowed = false
	mu.Unlock()
	if writeCount != 3 {
		t.Fatalf("expected exactly three upstream writes, got %d", writeCount)
	}
	if err := remote.SyncWorkbench(firstConn.ID); err != nil {
		t.Fatal(err)
	}
	wait("revoked snapshot to become unavailable", func() bool {
		_, err := remote.WorkbenchTask(created.ID)
		return err != nil && common.ProtocolError(err).Code == "NOT_FOUND"
	})
	if _, err := remote.WorkbenchTaskOperationReceipt("create"); err == nil || common.ProtocolError(err).Code != "NOT_FOUND" {
		t.Fatalf("revoked receipt is still visible: %v", err)
	}
	revoked, err := remote.WorkbenchSyncDetails(firstConn.ID)
	if err != nil || revoked.AccessState != "unavailable" || len(revoked.Attempts) != 0 || revoked.LastSuccess != nil {
		t.Fatalf("same-account revocation leaked old sync data: %+v %v", revoked, err)
	}
	otherDetails, err := remote.WorkbenchSyncDetails(secondConn.ID)
	if err != nil || len(otherDetails.Attempts) == 0 || otherDetails.LastSuccess == nil {
		t.Fatalf("revocation changed another repository's history: %+v %v", otherDetails, err)
	}
	other, err := remote.WorkbenchTasks(common.WorkbenchQuery{Connection: secondConn.ID, Limit: 5})
	if err != nil || len(other.Items) != 1 || other.Items[0].ID == created.ID || other.Items[0].NativeID != "7" || !strings.Contains(other.Items[0].Title, "Second repo") {
		t.Fatalf("second repository changed: %+v, %v", other, err)
	}
	mu.Lock()
	firstAllowed = true
	mu.Unlock()
	if err := remote.SyncWorkbench(firstConn.ID); err != nil {
		t.Fatal(err)
	}
	waitSync(firstConn.ID, "full")
	if restored, err := remote.WorkbenchTask(created.ID); err != nil || restored.ID != created.ID || restored.State != "open" {
		t.Fatalf("reauthorized task: %+v, %v", restored, err)
	}
	mu.Lock()
	accountID = 18
	mu.Unlock()
	switched, err := remote.ConfigureWorkbenchGitHub(common.WorkbenchConnection{
		ID: firstConn.ID, Version: firstConn.Version, Kind: common.WorkbenchGitHubRepository,
		Name: "First", Repository: "owner/first",
	}, accessToken, true)
	if err != nil {
		t.Fatal(err)
	}
	waitSync(switched.ID, "full")
	details, err := remote.WorkbenchSyncDetails(switched.ID)
	if err != nil || details.AccountLogin != "account-18" || len(details.Attempts) != 1 || details.Attempts[0].SyncType != "full" {
		t.Fatalf("new account received old attempts: %+v %v", details, err)
	}
	if _, err := remote.WorkbenchTaskOperationReceipt("create"); err == nil || common.ProtocolError(err).Code != "NOT_FOUND" {
		t.Fatalf("new account can access old receipt: %v", err)
	}
	sibling, err := remote.WorkbenchSyncDetails(secondConn.ID)
	if err != nil || sibling.AccountLogin != "account-17" || len(sibling.Attempts) == 0 {
		t.Fatalf("sibling history was affected: %+v %v", sibling, err)
	}
}
