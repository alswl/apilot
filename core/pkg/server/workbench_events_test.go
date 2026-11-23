//go:build darwin || linux

package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/services"
)

func TestWorkbenchEventStreamCommitRestartAndShutdown(t *testing.T) {
	dir := t.TempDir()
	runtime, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	events := make(chan common.WorkbenchEvent, 4)
	done := make(chan error, 1)
	remote := services.NewRemote(runtime.SocketPath)
	go func() {
		done <- remote.WatchWorkbenchEvents(ctx, func(event common.WorkbenchEvent) error {
			events <- event
			return nil
		})
	}()
	next := func() common.WorkbenchEvent {
		t.Helper()
		select {
		case event := <-events:
			return event
		case err := <-done:
			t.Fatalf("stream stopped: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		return common.WorkbenchEvent{}
	}
	initial := next()
	if initial.Type != "snapshot" {
		t.Fatalf("initial: %+v", initial)
	}
	connections, err := remote.WorkbenchConnections()
	if err != nil || len(connections) != 1 {
		t.Fatalf("connections: %+v %v", connections, err)
	}
	conn := connections[0]
	receipt, err := remote.SubmitWorkbenchTaskOperation(common.WorkbenchTaskWrite{
		RequestID: "event-test", ConnectionID: conn.ID, ConnectionVersion: conn.Version, Title: "private task title",
	})
	if err != nil || receipt.SourceObjectID == "" {
		t.Fatalf("create: %+v %v", receipt, err)
	}
	change := next()
	if change.Type != "changed" || change.Instance != initial.Instance || change.Sequence <= initial.Sequence {
		t.Fatalf("changed: %+v", change)
	}
	task, err := remote.WorkbenchTask(receipt.SourceObjectID)
	if err != nil || task.Title != "private task title" {
		t.Fatalf("read after commit event: %+v %v", task, err)
	}
	closed := make(chan error, 1)
	go func() { closed <- runtime.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("shutdown blocked on stream")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("stream survived service shutdown")
	}
	restarted, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	stop := errors.New("received snapshot")
	err = remote.WatchWorkbenchEvents(ctx, func(event common.WorkbenchEvent) error {
		if event.Type != "snapshot" || event.Instance == initial.Instance {
			t.Errorf("restart snapshot: %+v", event)
		}
		return stop
	})
	if !errors.Is(err, stop) {
		t.Fatalf("restart stream: %v", err)
	}
}
func TestWorkbenchEventStreamBoundsSubscribers(t *testing.T) {
	runtime, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	socket := filepath.Join(filepath.Dir(runtime.SocketPath), "http.sock")
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	get := func(path string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://apilot"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	const path = "/v1/workbench/events"
	streams := make([]*http.Response, 0, 32)
	for i := 0; i < cap(streams); i++ {
		resp := get(path)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("subscriber %d: %d", i, resp.StatusCode)
		}
		streams = append(streams, resp)
	}
	resp := get(path)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("limit response: %d %v", resp.StatusCode, resp.Header)
	}
	response := get("/ready")
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("subscriptions blocked normal reads: %d", response.StatusCode)
	}
	streams[0].Body.Close()
	for ctx.Err() == nil {
		resp := get(path)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("unexpected status after cancellation: %d", resp.StatusCode)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("closing subscriber did not release capacity")
}

type blockedEventWriter struct {
	header   http.Header
	deadline time.Time
}

func (w *blockedEventWriter) Header() http.Header       { return w.header }
func (w *blockedEventWriter) WriteHeader(int)           {}
func (w *blockedEventWriter) Write([]byte) (int, error) { return 0, context.DeadlineExceeded }
func (w *blockedEventWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline = deadline
	return nil
}

func TestWorkbenchEventWriteFailureReleasesSlot(t *testing.T) {
	events := newWorkbenchEvents(t.Context())
	writer := &blockedEventWriter{header: make(http.Header)}
	events.serveHTTP(writer, httptest.NewRequest(http.MethodGet, "/v1/workbench/events", nil))
	deadline := time.Until(writer.deadline)
	if len(events.slots) != 0 || deadline <= 0 || deadline > 5*time.Second {
		t.Fatalf("deadline=%v active subscribers=%d", deadline, len(events.slots))
	}
}

func TestWorkbenchShutdownDrainsOrdinaryRequestAfterStream(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := newWorkbenchEvents(ctx)
	entered := make(chan struct{})
	release := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/events" {
			events.serveHTTP(w, r)
			return
		}
		close(entered)
		<-release
		w.WriteHeader(http.StatusAccepted)
	}))
	defer fixture.Close()
	h := &workbenchHTTP{server: fixture.Config, cancel: cancel, observed: make(chan struct{}), done: make(chan struct{})}
	close(h.observed)
	close(h.done)
	stream, err := fixture.Client().Get(fixture.URL + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	streamDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, stream.Body)
		streamDone <- err
	}()
	ordinary := make(chan error, 1)
	go func() {
		resp, err := fixture.Client().Get(fixture.URL + "/ordinary")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusAccepted {
				err = fmt.Errorf("ordinary request returned %d", resp.StatusCode)
			}
		}
		ordinary <- err
	}()
	<-entered
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	closed := make(chan struct{})
	go func() { h.close(); close(closed) }()
	select {
	case err := <-streamDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not stop before ordinary drain")
	}
	select {
	case err := <-ordinary:
		t.Fatalf("ordinary request closed early: %v", err)
	case <-closed:
		t.Fatal("shutdown cut short the ordinary request")
	case <-time.After(3200 * time.Millisecond):
	}
	close(release)
	if err := <-ordinary; err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after drain")
	}
}
