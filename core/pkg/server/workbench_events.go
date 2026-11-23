package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
)

type workbenchEvents struct {
	ctx      context.Context
	mu       sync.Mutex
	next     chan struct{}
	instance string
	seq      uint64
	slots    chan struct{}
}

func newWorkbenchEvents(ctx context.Context) *workbenchEvents {
	return &workbenchEvents{ctx: ctx, next: make(chan struct{}), instance: common.ID(), seq: 1, slots: make(chan struct{}, 32)}
}

func (e *workbenchEvents) observe(read func(context.Context) (int64, error), version int64, volatile func() uint64, volatileVersion uint64) {
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-tick.C:
			current, err := read(e.ctx)
			latestVolatile := volatile()
			if err != nil || current != version || latestVolatile != volatileVersion {
				e.mu.Lock()
				e.seq++
				close(e.next)
				e.next = make(chan struct{})
				e.mu.Unlock()
			}
			if err == nil {
				version = current
			}
			volatileVersion = latestVolatile
		}
	}
}

func (e *workbenchEvents) snapshot(kind string) (common.WorkbenchEvent, <-chan struct{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return common.WorkbenchEvent{Instance: e.instance, Sequence: e.seq, Type: kind}, e.next
}

func registerWorkbenchEventHTTP(router chi.Router, api huma.API, e *workbenchEvents) {
	path := workbenchPath + "/events"
	api.OpenAPI().AddOperation(&huma.Operation{
		OperationID: "watchWorkbenchEvents", Method: http.MethodGet, Path: path,
		Summary: "Subscribe to snapshot and change notices; reread visible state on each notice and reconnect",
		Responses: map[string]*huma.Response{
			"200": {Description: "Server-sent events. Each data line is a WorkbenchEvent JSON object.",
				Content: map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: "string"}}}},
			"503": {Description: "Event stream unavailable or subscriber limit reached"},
		},
	})
	router.Get(path, e.serveHTTP)
}

func (e *workbenchEvents) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if e == nil {
		http.Error(w, "Event stream unavailable", http.StatusServiceUnavailable)
		return
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	default:
		http.Error(w, "Event subscriber limit reached", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	controller := http.NewResponseController(w)
	write := func(format string, args ...any) bool {
		// Override the ordinary request deadline for each frame, including heartbeat writes.
		if err := controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return false
		}
		_, err := fmt.Fprintf(w, format, args...)
		if err != nil || controller.Flush() != nil {
			return false
		}
		return controller.SetWriteDeadline(time.Time{}) == nil
	}
	send := func(event common.WorkbenchEvent) bool {
		payload, _ := json.Marshal(event)
		return write("data: %s\n\n", payload)
	}
	event, next := e.snapshot("snapshot")
	if !send(event) {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-e.ctx.Done():
			return
		case <-heartbeat.C:
			if !write(": heartbeat\n\n") {
				return
			}
		case <-next:
			event, next = e.snapshot("changed")
			if !send(event) {
				return
			}
		}
	}
}
