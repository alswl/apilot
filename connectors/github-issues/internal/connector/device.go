package connector

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

// GitHubDeviceAuthorization is a short-lived authorization attempt. DeviceCode
// is secret and must never be exposed to the browser-facing UI or logs.
type GitHubDeviceAuthorization struct {
	VerificationURI string        `json:"verification_uri"`
	UserCode        string        `json:"user_code"`
	DeviceCode      string        `json:"-"`
	ExpiresAt       time.Time     `json:"expires_at"`
	Interval        time.Duration `json:"-"`
}

// GitHubUserCredential must be persisted only in protected credential storage.
type GitHubUserCredential struct {
	AccessToken      string    `json:"-"`
	RefreshToken     string    `json:"-"`
	ExpiresAt        time.Time `json:"expires_at,omitempty"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at,omitempty"`
}

type githubDeviceTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
	Error            string `json:"error"`
}

func (p githubDeviceTokenResponse) credential() (GitHubUserCredential, error) {
	if p.AccessToken == "" || !strings.EqualFold(p.TokenType, "bearer") || p.ExpiresIn < 0 || p.RefreshExpiresIn < 0 || p.ExpiresIn > 31_536_000 || p.RefreshExpiresIn > 31_536_000 || (p.ExpiresIn > 0 && (p.RefreshToken == "" || p.RefreshExpiresIn == 0)) {
		return GitHubUserCredential{}, protocol.Problem(protocol.Temporary, "account", "GitHub authorization returned an invalid response")
	}
	result := GitHubUserCredential{AccessToken: p.AccessToken, RefreshToken: p.RefreshToken}
	now := time.Now()
	if p.ExpiresIn > 0 {
		result.ExpiresAt = now.Add(time.Duration(p.ExpiresIn) * time.Second)
	}
	if p.RefreshExpiresIn > 0 {
		result.RefreshExpiresAt = now.Add(time.Duration(p.RefreshExpiresIn) * time.Second)
	}
	return result, nil
}

type GitHubDeviceClient struct {
	base     *url.URL
	clientID string
	http     *http.Client
}

func NewGitHubDeviceClient(baseURL, clientID string, transport http.RoundTripper) (*GitHubDeviceClient, error) {
	base, err := url.Parse(baseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") || strings.TrimSpace(clientID) == "" {
		return nil, protocol.Problem(protocol.InvalidConfig, "account", "Invalid GitHub authorization configuration")
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &GitHubDeviceClient{base: base, clientID: clientID, http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *GitHubDeviceClient) post(ctx context.Context, path string, values url.Values, out any) error {
	endpoint := *c.base
	endpoint.Path = path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return protocol.Problem(protocol.InvalidConfig, "account", "Invalid GitHub authorization request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return protocol.Problem(protocol.Temporary, "account", "GitHub authorization could not be reached")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return protocol.Problem(protocol.Temporary, "account", "GitHub authorization request failed")
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(out); err != nil {
		return protocol.Problem(protocol.Temporary, "account", "GitHub authorization returned an invalid response")
	}
	return nil
}

func (c *GitHubDeviceClient) Begin(ctx context.Context) (GitHubDeviceAuthorization, error) {
	var payload struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int64  `json:"expires_in"`
		Interval        int64  `json:"interval"`
	}
	err := c.post(ctx, "/login/device/code", url.Values{"client_id": {c.clientID}}, &payload)
	if err != nil {
		return GitHubDeviceAuthorization{}, err
	}
	verification, parseErr := url.Parse(payload.VerificationURI)
	if payload.DeviceCode == "" || payload.UserCode == "" || payload.ExpiresIn <= 0 || payload.ExpiresIn > 3600 || payload.Interval <= 0 || payload.Interval > payload.ExpiresIn || parseErr != nil || verification.Scheme != "https" || !strings.EqualFold(verification.Host, c.base.Host) || verification.User != nil || verification.Path != "/login/device" || verification.RawQuery != "" || verification.Fragment != "" {
		return GitHubDeviceAuthorization{}, protocol.Problem(protocol.Temporary, "account", "GitHub authorization returned an invalid response")
	}
	return GitHubDeviceAuthorization{VerificationURI: verification.String(), UserCode: payload.UserCode, DeviceCode: payload.DeviceCode, ExpiresAt: time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second), Interval: time.Duration(payload.Interval) * time.Second}, nil
}

// Poll waits for the user and never retries a successful token exchange.
func (c *GitHubDeviceClient) Poll(ctx context.Context, attempt GitHubDeviceAuthorization) (GitHubUserCredential, error) {
	if attempt.DeviceCode == "" || attempt.Interval <= 0 || attempt.ExpiresAt.IsZero() {
		return GitHubUserCredential{}, protocol.Problem(protocol.InvalidConfig, "account", "Invalid GitHub authorization attempt")
	}
	parent := ctx
	ctx, cancel := context.WithDeadline(ctx, attempt.ExpiresAt)
	defer cancel()
	interval := attempt.Interval
	for {
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			if parent.Err() != nil {
				return GitHubUserCredential{}, parent.Err()
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return GitHubUserCredential{}, protocol.ExpiredAuthorization("GitHub authorization expired")
			}
			return GitHubUserCredential{}, ctx.Err()
		case <-timer.C:
		}
		var payload githubDeviceTokenResponse
		err := c.post(ctx, "/login/oauth/access_token", url.Values{"client_id": {c.clientID}, "device_code": {attempt.DeviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}, &payload)
		if err != nil {
			if parent.Err() != nil {
				return GitHubUserCredential{}, parent.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return GitHubUserCredential{}, protocol.ExpiredAuthorization("GitHub authorization expired")
			}
			return GitHubUserCredential{}, err
		}
		switch payload.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "access_denied":
			return GitHubUserCredential{}, protocol.Problem(protocol.AccessDenied, "account", "GitHub authorization was declined")
		case "expired_token":
			return GitHubUserCredential{}, protocol.ExpiredAuthorization("GitHub authorization expired")
		case "":
			return payload.credential()
		default:
			return GitHubUserCredential{}, protocol.Problem(protocol.Temporary, "account", "GitHub authorization failed")
		}
	}
}

// Refresh exchanges a device-flow refresh token once. A lost response must not
// be retried blindly because GitHub rotates and invalidates both old tokens.
func (c *GitHubDeviceClient) Refresh(ctx context.Context, refreshToken string) (GitHubUserCredential, error) {
	if refreshToken == "" {
		return GitHubUserCredential{}, protocol.Problem(protocol.InvalidConfig, "account", "GitHub refresh token is missing")
	}
	var payload githubDeviceTokenResponse
	if err := c.post(ctx, "/login/oauth/access_token", url.Values{"client_id": {c.clientID}, "grant_type": {"refresh_token"}, "refresh_token": {refreshToken}}, &payload); err != nil {
		return GitHubUserCredential{}, err
	}
	if payload.Error != "" {
		return GitHubUserCredential{}, protocol.Problem(protocol.AuthRequired, "account", "GitHub authorization must be renewed")
	}
	return payload.credential()
}
