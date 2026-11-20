package integrations

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

type WorkbenchObsidianBridge struct {
	base   *url.URL
	token  string
	client *http.Client
}

func OpenWorkbenchObsidianBridge(endpoint, token, certificate string) (*WorkbenchObsidianBridge, error) {
	base, err := url.Parse(endpoint)
	if err != nil || base.Scheme != "https" || base.User != nil || base.Port() == "" ||
		(base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.Fragment != "" {
		return nil, common.Err("INVALID_ARGUMENT", "Use a loopback HTTPS Obsidian bridge endpoint with a port")
	}
	host := base.Hostname()
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return nil, common.Err("INVALID_ARGUMENT", "Obsidian bridge must listen on loopback")
	}
	pins := x509.NewCertPool()
	if token == "" || !pins.AppendCertsFromPEM([]byte(certificate)) {
		return nil, common.Err("INVALID_ARGUMENT", "Obsidian bridge requires a token and trusted certificate")
	}
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: pins, MinVersion: tls.VersionTLS12},
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if host == "localhost" {
				address = net.JoinHostPort("127.0.0.1", base.Port())
			}
			return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, address)
		},
	}
	return &WorkbenchObsidianBridge{base: base, token: token, client: &http.Client{
		Transport: transport, Timeout: 8 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (b *WorkbenchObsidianBridge) noteRequest(ctx context.Context, method, path string, content []byte, version string) (*http.Response, error) {
	if !workbenchVisiblePath(path) || !strings.EqualFold(filepath.Ext(path), ".md") {
		return nil, common.Err("INVALID_ARGUMENT", "Choose a Markdown note")
	}
	target := *b.base
	target.Path = "/vault/" + path
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(content))
	if err != nil {
		return nil, common.Err("INVALID_ARGUMENT", "Could not construct bridge request")
	}
	request.Header.Set("Authorization", "Bearer "+b.token)
	request.Header.Set("Accept", "text/markdown")
	if method == http.MethodPut {
		request.Header.Set("Content-Type", "text/markdown")
		request.Header.Set("If-Match", version)
	}
	response, err := b.client.Do(request)
	if err != nil {
		return nil, common.Err("IPC_UNAVAILABLE", "Obsidian bridge could not be reached")
	}
	return response, nil
}

func (b *WorkbenchObsidianBridge) ReadNote(ctx context.Context, path string) ([]byte, string, error) {
	response, err := b.noteRequest(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	version := response.Header.Get("ETag")
	if response.StatusCode == http.StatusOK && response.Header.Get("X-Apilot-Conditional-Write") != "vault-process-v1" {
		return nil, "", common.Err("CONFLICT", "Obsidian bridge does not support atomic conditional writes")
	}
	if response.StatusCode != http.StatusOK || !strongWorkbenchETag(version) {
		return nil, "", common.Err("CONFLICT", "Bridge did not return a versioned note")
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(data) > limit {
		return nil, "", common.Err("CONFLICT", "Bridge note exceeded the read limit or could not be read")
	}
	return data, version, nil
}

func strongWorkbenchETag(version string) bool {
	return len(version) > 2 && version[0] == '"' && version[len(version)-1] == '"' &&
		!strings.ContainsAny(version[1:len(version)-1], "\"\r\n")
}

func (b *WorkbenchObsidianBridge) ReplaceNote(ctx context.Context, path string, content []byte, version string) error {
	if !strongWorkbenchETag(version) {
		return common.Err("INVALID_ARGUMENT", "A source note version is required")
	}
	response, err := b.noteRequest(ctx, http.MethodPut, path, content, version)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusPreconditionFailed:
		return common.Err("STALE_REVISION", "Obsidian note changed before writing")
	default:
		return common.Err("CONFLICT", "Bridge did not confirm the note update")
	}
}

// VerifyVault proves the authenticated endpoint can see a fresh challenge
// written inside this connection's local scope.
func (b *WorkbenchObsidianBridge) VerifyVault(ctx context.Context, c common.WorkbenchConnection) error {
	root, err := openWorkbenchNoteRoot(c)
	if err != nil {
		return common.Err("CONFLICT", "Could not open the configured vault scope")
	}
	defer root.Close()
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	proof := hex.EncodeToString(nonce[:])
	name := "apilot-bridge-check-" + proof[:24] + ".md"
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return common.Err("CONFLICT", "Could not create a temporary vault verification note")
	}
	defer root.Remove(name)
	_, writeErr := io.WriteString(file, proof)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return common.Err("CONFLICT", "Could not write the vault verification note")
	}
	path := filepath.ToSlash(filepath.Join(c.Scope, name))
	observed, _, err := b.ReadNote(ctx, path)
	if err != nil {
		if problem, ok := err.(*common.Error); ok && problem.Message == "Obsidian bridge does not support atomic conditional writes" {
			return problem
		}
	}
	if err != nil || string(observed) != proof {
		return common.Err("CONFLICT", "Obsidian bridge could not verify the selected vault")
	}
	return nil
}
