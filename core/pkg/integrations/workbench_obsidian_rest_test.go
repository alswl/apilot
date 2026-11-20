package integrations

import (
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alswl/apilot/core/pkg/common"
)

func TestWorkbenchObsidianBridgeVerifiesVaultAndConditionalUpdate(t *testing.T) {
	vault := t.TempDir()
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(vault, "Note.md"), []byte("# Tasks\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/vault/")
		if !filepath.IsLocal(path) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		path = filepath.Join(vault, path)
		data, err := os.ReadFile(path)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		version := "\"revision\""
		if r.Method == http.MethodGet {
			w.Header().Set("X-Apilot-Conditional-Write", "vault-process-v1")
			w.Header().Set("ETag", version)
			_, _ = w.Write(data)
			return
		}
		if r.Method != http.MethodPut || r.Header.Get("If-Match") != version {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		content, err := io.ReadAll(r.Body)
		if err == nil {
			err = os.WriteFile(path, content, 0600)
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	bridge, err := OpenWorkbenchObsidianBridge(server.URL, "test-token", string(certificate))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := bridge.VerifyVault(ctx, common.WorkbenchConnection{Vault: vault}); err != nil {
		t.Fatal(err)
	}
	if err := bridge.VerifyVault(ctx, common.WorkbenchConnection{Vault: other}); err == nil {
		t.Fatal("verified a different vault")
	}
	entries, err := os.ReadDir(vault)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary verification note was not cleaned up: %v %v", entries, err)
	}
	_, version, err := bridge.ReadNote(ctx, "Note.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := bridge.ReplaceNote(ctx, "Note.md", []byte("invalid"), "\"stale\""); err == nil {
		t.Fatal("accepted stale source version")
	}
	replacement := []byte("# Tasks\n- [ ] Verify\n")
	if err := bridge.ReplaceNote(ctx, "Note.md", replacement, version); err != nil {
		t.Fatal(err)
	}
	data, _, err := bridge.ReadNote(ctx, "Note.md")
	if err != nil || string(data) != string(replacement) {
		t.Fatalf("readback: %q %v", data, err)
	}
}
