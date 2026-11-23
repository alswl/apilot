package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func testAPIResponse(request *http.Request, status int, body string, header http.Header) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestUC01GitHubLoginMatchesAccountAndRotatesCredential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	connector := New(t.TempDir())
	connector.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.github.com" || req.URL.Path != "/user" || req.Header.Get("Authorization") != "Bearer test-access" {
			t.Fatalf("unexpected API identity request: %s %s", req.Method, req.URL)
		}
		return testAPIResponse(req, 200, `{"id":42,"login":"octocat"}`, nil), nil
	})
	var rotated bool
	device, err := NewGitHubDeviceClient("https://github.com", appClientID, transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "github.com" || req.Method != "POST" {
			t.Fatalf("unexpected device request: %s %s", req.Method, req.URL)
		}
		switch req.URL.Path {
		case "/login/device/code":
			return testAPIResponse(req, 200, `{"device_code":"test-device","user_code":"TEST-CODE","verification_uri":"https://github.com/login/device","expires_in":10,"interval":1}`, nil), nil
		case "/login/oauth/access_token":
			if err := req.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if req.PostForm.Get("grant_type") == "refresh_token" {
				if req.PostForm.Get("refresh_token") != "test-refresh" {
					t.Fatal("previous refresh token was not used")
				}
				rotated = true
				return testAPIResponse(req, 200, `{"access_token":"test-next","refresh_token":"test-next-refresh","token_type":"bearer","expires_in":3600,"refresh_token_expires_in":86400}`, nil), nil
			}
			return testAPIResponse(req, 200, `{"access_token":"test-access","refresh_token":"test-refresh","token_type":"bearer","expires_in":30,"refresh_token_expires_in":86400}`, nil), nil
		default:
			return nil, fmt.Errorf("unexpected device path")
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	connector.device = device
	var stored []byte
	connector.loadCredential = func(_, ref string) ([]byte, error) {
		if ref != "42" {
			t.Fatalf("unexpected key reference %s", ref)
		}
		return stored, nil
	}
	connector.storeCredential = func(_, ref string, raw []byte) error {
		if ref != "42" {
			t.Fatalf("unexpected key reference %s", ref)
		}
		stored = raw
		return nil
	}
	notify := func(method string, params any) error {
		browser := params.(protocol.BrowserNotification)
		if method != "open_browser" || browser.RequestID != "login" || browser.UserCode != "TEST-CODE" {
			t.Fatalf("invalid browser event: %+v", browser)
		}
		return nil
	}
	if _, denied := connector.login(ctx, protocol.LoginParams{AccountRef: "99", EndpointRef: endpoint}, "login", notify); denied == nil || denied.Data.Kind != protocol.AccessDenied || stored != nil {
		t.Fatal("login for the wrong account changed storage")
	}
	account, problem := connector.login(ctx, protocol.LoginParams{AccountRef: "42", EndpointRef: endpoint}, "login", notify)
	if problem != nil || account.(protocol.Account).DisplayName != "octocat" || stored == nil {
		t.Fatalf("authorization: %+v %v", account, problem)
	}
	token, problem := connector.token(ctx, "42", endpoint)
	if problem != nil || !rotated || token != "test-next" {
		t.Fatalf("credential rotation failed: %v", problem)
	}
}

func TestUC01GitHubSyncPagesExcludePullRequestsAndBindCheckpoint(t *testing.T) {
	connector := New(t.TempDir())
	connector.loadCredential = func(string, string) ([]byte, error) { return json.Marshal(storedCredential{Access: "test"}) }
	var calls int
	connector.transport = transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != apiHost || req.URL.Path != "/repositories/7/issues" || req.URL.Query().Get("state") != "all" {
			t.Fatalf("wrong sync target: %s", req.URL)
		}
		if req.URL.Query().Get("since") != "" {
			return testAPIResponse(req, 200, `[]`, nil), nil
		}
		if req.URL.Query().Get("page") == "1" {
			next := *req.URL
			query := next.Query()
			query.Set("page", "2")
			next.RawQuery = query.Encode()
			header := http.Header{"Link": {`<` + next.String() + `>; rel="next"`}}
			return testAPIResponse(req, 200, `[{"id":42,"number":1,"title":"Issue","state":"open","updated_at":"2026-11-23T12:00:00Z"},{"id":44,"number":2,"pull_request":{}}]`, header), nil
		}
		return testAPIResponse(req, 200, `[{"id":43,"number":3,"title":"Closed","state":"closed","state_reason":"completed","updated_at":"2026-11-23T12:00:00Z"}]`, nil), nil
	})
	ctx := context.Background()
	p := protocol.SyncParams{ScopeContext: protocol.ScopeContext{AccountRef: "42", EndpointRef: endpoint, ScopeRef: "7"}, Mode: "full"}
	raw, err := connector.sync(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	first := raw.(protocol.SyncPage)
	if len(first.Records) != 1 || first.Records[0].SourceID != "42" || first.NextPage == "" || first.Usage.Received != 1 {
		t.Fatalf("first page: %+v", first)
	}
	p.Page = first.NextPage
	raw, err = connector.sync(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	last := raw.(protocol.SyncPage)
	if len(last.Records) != 1 || last.Records[0].State != "done" || last.Coverage != "full" || last.NextCheckpoint == "" {
		t.Fatalf("last page: %+v", last)
	}
	p.Mode, p.Page, p.Checkpoint = "delta", "", last.NextCheckpoint
	raw, err = connector.sync(ctx, p)
	if err != nil || len(raw.(protocol.SyncPage).Records) != 0 || raw.(protocol.SyncPage).Coverage != "delta" {
		t.Fatalf("delta: %+v, %v", raw, err)
	}
	p.ScopeRef = "8"
	if _, err := connector.sync(ctx, p); err == nil || err.Data.Kind != protocol.InvalidCheckpoint || calls != 3 {
		t.Fatal("checkpoint was accepted for a different repository")
	}
}
