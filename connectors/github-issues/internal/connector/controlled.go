//go:build apilot_controlled_github

package connector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type controlledSettings struct {
	Origin  string `json:"origin"`
	RootPEM string `json:"root_pem"`
}

var invalidControlledOrigin = errors.New("controlled GitHub requires a loopback HTTPS origin and fixture certificate")

// NewControlled selects a closed, loopback-only transport and a disposable
// credential file. The production binary does not compile this constructor.
func NewControlled(dir string) (*Connector, error) {
	if !filepath.IsAbs(dir) {
		return nil, invalidControlledOrigin
	}
	raw, err := os.ReadFile(filepath.Join(dir, "controlled-github.json"))
	if err != nil {
		return nil, errors.New("controlled GitHub settings are required")
	}
	var config controlledSettings
	if json.Unmarshal(raw, &config) != nil {
		return nil, invalidControlledOrigin
	}
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Scheme != "https" || !net.ParseIP(origin.Hostname()).IsLoopback() ||
		origin.User != nil || origin.Port() == "" || origin.Path != "" || origin.RawQuery != "" ||
		origin.ForceQuery || origin.Fragment != "" || origin.Opaque != "" {
		return nil, invalidControlledOrigin
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(config.RootPEM)) {
		return nil, invalidControlledOrigin
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != origin.Host {
				return nil, invalidControlledOrigin
			}
			return dialer.DialContext(ctx, network, address)
		},
	}
	rewrite := controlledTransport{origin: origin, transport: transport}
	device, err := NewGitHubDeviceClient(origin.String(), "apilot-controlled", rewrite)
	if err != nil {
		return nil, err
	}
	credential := filepath.Join(dir, "controlled-credential.json")
	return &Connector{
		dir: dir, device: device, transport: rewrite,
		loadCredential: func(_, _ string) ([]byte, error) {
			data, err := os.ReadFile(credential)
			if errors.Is(err, os.ErrNotExist) {
				return nil, errNoCredential
			}
			return data, err
		},
		storeCredential: func(_, _ string, data []byte) error {
			return os.WriteFile(credential, data, 0600)
		},
	}, nil
}

type controlledTransport struct {
	origin    *url.URL
	transport *http.Transport
}

func (c controlledTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.User != nil ||
		(request.URL.Host != apiHost && request.URL.Host != c.origin.Host) {
		return nil, invalidControlledOrigin
	}
	clone := request.Clone(request.Context())
	clone.URL.Host = c.origin.Host
	clone.Host = c.origin.Host
	return c.transport.RoundTrip(clone)
}
