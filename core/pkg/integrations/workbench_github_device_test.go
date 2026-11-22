package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestGitHubDeviceAuthorizationPendingThenSuccess(t *testing.T) {
	var polls atomic.Int32
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Form.Get("client_id") != "public-client" {
			t.Error("missing client ID")
		}
		switch r.URL.Path {
		case "/login/device/code":
			fmt.Fprintf(w, `{"device_code":"device-secret","user_code":"ABCD-1234","verification_uri":%q,"expires_in":60,"interval":1}`, server.URL+"/login/device")
		case "/login/oauth/access_token":
			if r.Form.Get("device_code") != "device-secret" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Error("invalid poll")
			}
			if polls.Add(1) == 1 {
				fmt.Fprint(w, `{"error":"authorization_pending"}`)
			} else {
				fmt.Fprint(w, `{"access_token":"access-secret","token_type":"bearer"}`)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := NewGitHubDeviceClient(server.URL, "public-client", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := client.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	public, err := json.Marshal(attempt)
	if err != nil || strings.Contains(string(public), "device-secret") {
		t.Fatalf("device secret exposed: %s %v", public, err)
	}
	attempt.Interval = time.Millisecond
	token, err := client.Poll(context.Background(), attempt)
	if err != nil || token.AccessToken != "access-secret" || polls.Load() != 2 {
		t.Fatalf("poll failed: polls=%d err=%v", polls.Load(), err)
	}
}

func TestGitHubDeviceAuthorizationRejectsExternalURLAndCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"device_code":"secret","user_code":"ABCD","verification_uri":"https://attacker.example/login/device","expires_in":60,"interval":1}`)
	}))
	defer server.Close()
	client, err := NewGitHubDeviceClient(server.URL, "client", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Begin(context.Background()); err == nil {
		t.Fatal("accepted external verification URL")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Poll(ctx, GitHubDeviceAuthorization{DeviceCode: "secret", ExpiresAt: time.Now().Add(time.Minute), Interval: time.Second}); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestGitHubDeviceRefreshRotatesCredentials(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("client_secret") != "" {
			t.Error("invalid refresh request")
		}
		fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"new-refresh","token_type":"bearer","expires_in":28800,"refresh_token_expires_in":15897600}`)
	}))
	defer server.Close()
	client, err := NewGitHubDeviceClient(server.URL, "client", server.Client().Transport)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := client.Refresh(context.Background(), "old-refresh")
	if err != nil || credential.AccessToken != "new-access" || credential.RefreshToken != "new-refresh" || credential.ExpiresAt.IsZero() || credential.RefreshExpiresAt.IsZero() || calls.Load() != 1 {
		t.Fatalf("refresh: calls=%d err=%v", calls.Load(), err)
	}
	public, err := json.Marshal(credential)
	if err != nil || strings.Contains(string(public), "new-access") || strings.Contains(string(public), "new-refresh") {
		t.Fatalf("credential was exposed in JSON: %s %v", public, err)
	}
}

func TestGitHubDevicePollDenialExpiryAndSlowDown(t *testing.T) {
	for _, tc := range []struct{ name, response, want string }{
		{"denied", "access_denied", "AUTHORIZATION_DENIED"},
		{"expired", "expired_token", "AUTHORIZATION_EXPIRED"},
		{"slow-down", "slow_down", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				fmt.Fprintf(w, `{"error":%q}`, tc.response)
			}))
			defer server.Close()
			client, err := NewGitHubDeviceClient(server.URL, "client", server.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err = client.Poll(ctx, GitHubDeviceAuthorization{DeviceCode: "secret", ExpiresAt: time.Now().Add(time.Minute), Interval: time.Millisecond})
			var problem *common.Error
			if tc.want == "" && errors.Is(err, context.DeadlineExceeded) && calls.Load() == 1 {
				return
			}
			if !errors.As(err, &problem) || problem.Code != tc.want || calls.Load() != 1 {
				t.Fatalf("result=%v calls=%d", err, calls.Load())
			}
		})
	}
}
