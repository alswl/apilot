//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/config"
)

func TestConnectorLoginInterruptCancelsBlockedStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	socket := config.HTTPSocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	polling, released, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/connector-authorizations"):
			_, _ = io.WriteString(w, `{"id":"login","status":"waiting"}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/connector-authorizations/login"):
			close(polling)
			<-r.Context().Done()
			close(released)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/connector-authorizations/login"):
			close(cancelled)
			_, _ = io.WriteString(w, `{"id":"login","status":"cancelled"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	bin := filepath.Join(t.TempDir(), "apilot")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	login := exec.CommandContext(ctx, bin, "workbench", "connector", "login", "fixture")
	if err := login.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- login.Wait() }()
	select {
	case <-polling:
	case <-ctx.Done():
		t.Fatal("login status poll never started")
	}
	if err := login.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 5 {
			t.Fatalf("cancelled login exit: %v", err)
		}
	case <-timer.C:
		t.Fatal("login did not exit before the HTTP timeout")
	}
	select {
	case <-released:
	case <-timer.C:
		t.Fatal("blocked HTTP status request was not released")
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("login session cancellation did not reach Core")
	}
}
