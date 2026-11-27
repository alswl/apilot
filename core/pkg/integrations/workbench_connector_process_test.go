package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
)

func testWorkbenchConnector(t *testing.T, body string) WorkbenchConnectorPackage {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := []byte("#!/bin/sh\nread -r request\n" + body + "\n")
	manifest := protocol.Manifest{
		ManifestVersion: 1, ConnectorID: "fixture", ReleaseVersion: "0.1.0", ProtocolVersions: []int{1},
		OS: runtime.GOOS, Arch: runtime.GOARCH, Entrypoint: "connector",
		EntrypointSHA256: fmt.Sprintf("%x", sha256.Sum256(binary)),
	}
	if err := os.WriteFile(filepath.Join(dir, "connector"), binary, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	pkg, err := LoadWorkbenchConnectorPackage(filename, dir)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestWorkbenchConnectorProcess(t *testing.T) {
	pkg := testWorkbenchConnector(t, `
printf '%s\n' diagnostic >&2
printf '%s\n' '{"jsonrpc":"2.0","method":"open_browser","params":{"request_id":"call","url":"https://example.test/login"}}'
printf '%s\n' '{"jsonrpc":"2.0","id":"call","result":{"account_ref":"acct"}}'
`)
	process := NewWorkbenchConnectorProcess(pkg, t.TempDir())
	var notification string
	result, err := process.Call(context.Background(), protocol.Request{ID: "call", Method: "auth.login"}, func(msg protocol.Message) error {
		notification = msg.Method
		return nil
	})
	if err != nil || notification != "open_browser" || string(result) != `{"account_ref":"acct"}` {
		t.Fatalf("response %s, notification %s, error %v", result, notification, err)
	}

	if err := os.WriteFile(pkg.entrypoint, []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := process.Call(context.Background(), protocol.Request{ID: "call", Method: "describe"}, nil); !errors.Is(err, ErrWorkbenchConnectorProcess) {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestWorkbenchConnectorRejectsInvalidResponses(t *testing.T) {
	request := protocol.Request{ID: "call", Method: "auth.login"}
	ok := `{"jsonrpc":"2.0","id":"call","result":null}` + "\n"
	for name, frame := range map[string]string{
		"missing terminal":    "",
		"duplicate terminal":  ok + ok,
		"wrong ID":            strings.Replace(ok, "call", "other", 1),
		"batch":               "[]\n",
		"invalid UTF-8":       "\xff\n",
		"unterminated":        strings.TrimSuffix(ok, "\n"),
		"oversize":            strings.Repeat(" ", protocol.MaxFrame+1) + "\n",
		"login limit":         `{"jsonrpc":"2.0","method":"notice","params":"` + strings.Repeat("a", protocol.MaxLoginNotification) + "\"}\n" + ok,
		"browser credentials": `{"jsonrpc":"2.0","method":"open_browser","params":{"request_id":"call","url":"https://name:password@example.test/"}}` + "\n" + ok,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readWorkbenchConnectorResponse(strings.NewReader(frame), request, nil); !errors.Is(err, ErrWorkbenchConnectorProtocol) {
				t.Fatalf("expected protocol error, got %v", err)
			}
		})
	}
}

func TestWorkbenchConnectorProcessDeadline(t *testing.T) {
	pkg := testWorkbenchConnector(t, "/bin/sleep 30 & wait")
	process := NewWorkbenchConnectorProcess(pkg, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := process.Call(ctx, protocol.Request{ID: "call", Method: "tasks.sync"}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("process group did not stop promptly")
	}
}

func TestWorkbenchConnectorFullSyncDeadline(t *testing.T) {
	pkg := testWorkbenchConnector(t, `printf '{"jsonrpc":"2.0","id":"call","result":{"deadline":"%s"}}\n' "$APILOT_CONNECTOR_DEADLINE"`)
	process := NewWorkbenchConnectorProcess(pkg, t.TempDir())
	t.Cleanup(process.Close)
	result, err := process.Call(context.Background(), protocol.Request{ID: "call", Method: "tasks.sync"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Deadline time.Time `json:"deadline"`
	}
	if err := json.Unmarshal(result, &response); err != nil {
		t.Fatal(err)
	}
	remaining := time.Until(response.Deadline)
	if remaining < 9*time.Minute || remaining > 10*time.Minute {
		t.Fatalf("full scan must have the service's ten-minute budget; got %v", remaining)
	}
}

func TestWorkbenchConnectorProcessClose(t *testing.T) {
	pkg := testWorkbenchConnector(t, `
printf '%s\n' '{"jsonrpc":"2.0","method":"sync.usage","params":{}}'
/bin/sleep 30 &
wait
`)
	process := NewWorkbenchConnectorProcess(pkg, t.TempDir())
	t.Cleanup(process.Close)
	reading := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := process.Call(context.Background(), protocol.Request{ID: "call", Method: "tasks.sync"}, func(protocol.Message) error {
			close(reading)
			return nil
		})
		done <- err
	}()
	select {
	case <-reading:
	case err := <-done:
		t.Fatalf("connector exited before sending usage: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("connector did not send usage")
	}
	process.Close()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("active call after close: %v", err)
	}
	if _, err := process.Call(context.Background(), protocol.Request{ID: "call", Method: "describe"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("new call after close: %v", err)
	}
}

func TestWorkbenchConnectorDiscoversOnlyBundledGitHub(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS bundle layout")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	contents := filepath.Join(dir, "Apilot.app", "Contents")
	manifests := filepath.Join(contents, "Resources", "connectors")
	helpers := filepath.Join(contents, "Helpers", "connectors")
	for _, path := range []string{manifests, helpers} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	binary := []byte(`#!/bin/sh
read -r request
printf '%s\n' '{"jsonrpc":"2.0","id":"describe","result":{"connector_id":"github-issues","release_version":"0.1.0","protocol_versions":[1],"endpoint_config":{"version":1,"schema":{"type":"object"}},"scope_config":{"version":1,"schema":{"type":"object"}}}}'
`)
	manifest := protocol.Manifest{
		ManifestVersion: 1, ConnectorID: "github-issues", ReleaseVersion: "0.1.0",
		ProtocolVersions: []int{1}, OS: runtime.GOOS, Arch: runtime.GOARCH,
		Entrypoint: "apilot-github-issues", EntrypointSHA256: fmt.Sprintf("%x", sha256.Sum256(binary)),
	}
	if err := os.WriteFile(filepath.Join(helpers, manifest.Entrypoint), binary, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifests, "github-issues.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifests, "fixture.json"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}

	packages, err := DiscoverBundledWorkbenchConnectors(filepath.Join(contents, "MacOS", "Apilot"))
	if err != nil || len(packages) != 1 || packages[0].Digest != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatalf("discovered packages: %v, %v", packages, err)
	}
	process := NewWorkbenchConnectorProcess(packages[0], t.TempDir())
	defer process.Close()
	if desc, err := process.Describe(context.Background()); err != nil || desc.ConnectorID != manifest.ConnectorID {
		t.Fatalf("describe: %+v, %v", desc, err)
	}

	packages[0].Manifest.ReleaseVersion = "2.0.0"
	mismatch := NewWorkbenchConnectorProcess(packages[0], t.TempDir())
	defer mismatch.Close()
	if _, err := mismatch.Describe(context.Background()); !errors.Is(err, ErrWorkbenchConnectorProtocol) {
		t.Fatalf("expected identity mismatch, got %v", err)
	}
}
