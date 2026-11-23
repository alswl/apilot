//go:build darwin || linux

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/config"
	"golang.org/x/sys/unix"
)

func TestWorkbenchWatchBinaryReconnectAndInterrupt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	socket := config.HTTPSocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "apilot")
	if output, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	once := exec.Command(bin, "--json", "workbench", "watch", "--once")
	if output, err := once.CombinedOutput(); !errors.As(err, new(*exec.ExitError)) || once.ProcessState.ExitCode() != 3 {
		t.Fatalf("missing socket --once: %v %s", err, output)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	watch := exec.CommandContext(ctx, bin, "workbench", "watch")
	stdout, err := watch.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	defer watch.Wait()
	time.Sleep(100 * time.Millisecond)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var requests, forcedStatus atomic.Int32
	active, released := make(chan struct{}, 8), make(chan struct{}, 8)
	rotate := make(chan struct{})
	rejected := make(chan struct{}, 4)
	fixture := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requests.Add(1)
		if forced := forcedStatus.Load(); forced != 0 {
			http.Error(w, "fixture", int(forced))
			if forced == http.StatusConflict {
				w.(http.Flusher).Flush()
				rejected <- struct{}{}
			}
			return
		}
		if count == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"instance\":\"instance-%d\",\"sequence\":1,\"type\":\"snapshot\"}\n\n", count)
		w.(http.Flusher).Flush()
		if count > 2 {
			active <- struct{}{}
			select {
			case <-r.Context().Done():
			case <-rotate:
			}
			released <- struct{}{}
		}
	})}
	go func() { _ = fixture.Serve(listener) }()
	defer fixture.Close()
	decoder := json.NewDecoder(bufio.NewReader(stdout))
	for _, want := range []string{"instance-2", "instance-3"} {
		var response struct{ Data common.WorkbenchEvent }
		if err := decoder.Decode(&response); err != nil || response.Data.Instance != want || response.Data.Type != "snapshot" {
			t.Fatalf("reconnect: %+v %v, want %s", response, err, want)
		}
	}
	if err := watch.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("first subscription did not release")
	}
	<-active

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	fillWatchPipe(t, int(writer.Fd()))
	blocked := exec.CommandContext(ctx, bin, "workbench", "watch")
	blocked.Stdout = writer
	if err := blocked.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- blocked.Wait() }()
	select {
	case <-active:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked writer did not establish subscription")
	}
	// Let the first frame reach the inherited, full pipe. The reader is kept
	// open and untouched until after the child has exited.
	time.Sleep(100 * time.Millisecond)
	if err := blocked.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGINT did not exit with a full stdout pipe")
	}
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked writer left the subscription open")
	}
	for _, tc := range []struct {
		status int32
		once   bool
		code   string
	}{
		{http.StatusServiceUnavailable, true, "IPC_UNAVAILABLE"},
		{http.StatusConflict, false, "PROTOCOL_MISMATCH"},
	} {
		forcedStatus.Store(tc.status)
		prior := requests.Load()
		args := []string{"--json", "workbench", "watch"}
		if tc.once {
			args = append(args, "--once")
		}
		command := exec.CommandContext(ctx, bin, args...)
		output, err := command.CombinedOutput()
		var exit *exec.ExitError
		var response struct{ Error *common.Error }
		if !errors.As(err, &exit) || exit.ExitCode() != 6 || json.Unmarshal(output, &response) != nil ||
			response.Error == nil || response.Error.Code != tc.code || requests.Load() != prior+1 {
			t.Fatalf("HTTP %d: expected one terminal request and %s, got %v %s", tc.status, tc.code, err, output)
		}
	}

	<-rejected
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderrRead.Close()
	defer stderrWrite.Close()
	fillWatchPipe(t, int(stderrWrite.Fd()))
	forcedStatus.Store(0)
	conflict := exec.CommandContext(ctx, bin, "workbench", "watch")
	conflict.Stderr = stderrWrite
	if err := conflict.Start(); err != nil {
		t.Fatal(err)
	}
	conflictDone := make(chan error, 1)
	go func() { conflictDone <- conflict.Wait() }()
	select {
	case <-active:
	case <-time.After(2 * time.Second):
		t.Fatal("stderr case did not subscribe")
	}
	forcedStatus.Store(http.StatusConflict)
	close(rotate)
	select {
	case <-released:
	case <-time.After(2 * time.Second):
		t.Fatal("previous subscription did not end")
	}
	select {
	case <-rejected:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnection did not receive HTTP 409")
	}
	time.Sleep(100 * time.Millisecond)
	if err := conflict.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-conflictDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIGINT did not exit with a full stderr pipe after HTTP 409")
	}

}

func fillWatchPipe(t *testing.T, fd int) {
	t.Helper()
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{4096, 1} {
		data := make([]byte, size)
		for {
			_, err := unix.Write(fd, data)
			if errors.Is(err, syscall.EAGAIN) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		t.Fatal(err)
	}
}
