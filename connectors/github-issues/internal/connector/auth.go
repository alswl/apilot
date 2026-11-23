package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

const endpoint = "github.com"

var appClientID = "Iv23li9W6zajXFdAUnvh"
var appSlug = "apilot-app"
var errNoCredential = errors.New("GitHub credential not found")

type Connector struct {
	dir             string
	device          *GitHubDeviceClient
	transport       http.RoundTripper
	loadCredential  func(string, string) ([]byte, error)
	storeCredential func(string, string, []byte) error
}

func New(dir string) *Connector {
	device, _ := NewGitHubDeviceClient("https://github.com", appClientID, nil)
	return &Connector{dir: dir, device: device, loadCredential: readCredential, storeCredential: writeCredential}
}

func (c *Connector) Handle(ctx context.Context, req protocol.Request, notify protocol.Notify) (any, *protocol.RPCError) {
	switch req.Method {
	case protocol.Login:
		var p protocol.LoginParams
		if e := protocol.DecodeParams(req, &p); e != nil {
			return nil, e
		}
		return c.login(ctx, p, req.ID, notify)
	case protocol.Options:
		var p protocol.OptionsParams
		if e := protocol.DecodeParams(req, &p); e != nil {
			return nil, e
		}
		return c.options(ctx, p)
	case protocol.Validate:
		var p protocol.ValidateParams
		if e := protocol.DecodeParams(req, &p); e != nil {
			return nil, e
		}
		return c.validate(ctx, p)
	case protocol.Sync:
		var p protocol.SyncParams
		if e := protocol.DecodeParams(req, &p); e != nil {
			return nil, e
		}
		return c.sync(ctx, p)
	case protocol.Resolve:
		var p protocol.ResolveParams
		if e := protocol.DecodeParams(req, &p); e != nil {
			return nil, e
		}
		return c.resolve(ctx, p)
	default:
		return nil, protocol.Problem(protocol.Unsupported, "", "Method not supported")
	}
}

func (c *Connector) login(ctx context.Context, p protocol.LoginParams, id string, notify protocol.Notify) (any, *protocol.RPCError) {
	if c.dir == "" || c.device == nil || (p.EndpointRef != "" && p.EndpointRef != endpoint) {
		return nil, protocol.Problem(protocol.InvalidConfig, "endpoint", "GitHub.com configuration is required")
	}
	attempt, err := c.device.Begin(ctx)
	if err != nil {
		return nil, authProblem(err)
	}
	if err := notify("open_browser", protocol.BrowserNotification{RequestID: id, URL: attempt.VerificationURI, UserCode: attempt.UserCode}); err != nil {
		return nil, authProblem(err)
	}
	credential, err := c.device.Poll(ctx, attempt)
	if err != nil {
		return nil, authProblem(err)
	}
	var identity struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	client := newAPI(credential.AccessToken, c.transport)
	if _, problem := client.get(ctx, "/user", nil, &identity); problem != nil {
		return nil, problem
	}
	if identity.ID <= 0 || identity.Login == "" {
		return nil, authProblem(errors.New("Invalid GitHub identity"))
	}
	account := strconv.FormatInt(identity.ID, 10)
	if p.AccountRef != "" && p.AccountRef != account {
		return nil, protocol.Problem(protocol.AccessDenied, "account", "Sign in to the original GitHub account")
	}
	release, err := c.lockAccount(ctx, account)
	if err != nil {
		return nil, authProblem(err)
	}
	defer release()
	if err := c.saveCredential(account, credential); err != nil {
		return nil, authProblem(err)
	}
	return protocol.Account{AccountRef: account, EndpointRef: endpoint, DisplayName: identity.Login}, nil
}

type storedCredential struct {
	Access         string
	Refresh        string
	Expires        time.Time
	RefreshExpires time.Time
}

func (c *Connector) namespace() string {
	return fmt.Sprintf("com.alswl.apilot.connector.github-issues.%x", sha256.Sum256([]byte(c.dir)))
}

func (c *Connector) saveCredential(account string, credential GitHubUserCredential) error {
	payload, err := json.Marshal(storedCredential{credential.AccessToken, credential.RefreshToken, credential.ExpiresAt, credential.RefreshExpiresAt})
	if err != nil {
		return err
	}
	return c.storeCredential(c.namespace(), account, payload)
}

func (c *Connector) token(ctx context.Context, account, site string) (string, *protocol.RPCError) {
	if !positiveID(account) || site != endpoint || c.dir == "" || c.device == nil {
		return "", protocol.Problem(protocol.InvalidConfig, "account", "Invalid GitHub account context")
	}
	release, err := c.lockAccount(ctx, account)
	if err != nil {
		return "", authProblem(err)
	}
	defer release()
	raw, err := c.loadCredential(c.namespace(), account)
	if errors.Is(err, errNoCredential) {
		return "", renewAuthorization()
	}
	if err != nil {
		return "", authProblem(err)
	}
	var stored storedCredential
	if json.Unmarshal(raw, &stored) != nil || stored.Access == "" {
		return "", renewAuthorization()
	}
	if stored.Expires.IsZero() || time.Until(stored.Expires) > time.Minute {
		return stored.Access, nil
	}
	if stored.Refresh == "" || (!stored.RefreshExpires.IsZero() && time.Now().After(stored.RefreshExpires)) {
		return "", renewAuthorization()
	}
	refreshed, err := c.device.Refresh(ctx, stored.Refresh)
	if err != nil {
		return "", authProblem(err)
	}
	if err := c.saveCredential(account, refreshed); err != nil {
		return "", authProblem(err)
	}
	return refreshed.AccessToken, nil
}

func (c *Connector) lockAccount(ctx context.Context, account string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(c.dir, account+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

func positiveID(raw string) bool {
	id, err := strconv.ParseInt(raw, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == raw
}

func renewAuthorization() *protocol.RPCError {
	return protocol.Problem(protocol.AuthRequired, "account", "GitHub authorization must be renewed")
}

func authProblem(err error) *protocol.RPCError {
	var problem *protocol.RPCError
	if errors.As(err, &problem) {
		return problem
	}
	return protocol.Problem(protocol.Temporary, "account", "GitHub authorization is temporarily unavailable")
}
