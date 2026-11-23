//go:build darwin || linux

package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/dal"
	"github.com/alswl/apilot/core/pkg/services"
)

type workbenchListener struct{ *net.UnixListener }

func (l workbenchListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.AcceptUnix()
		if err != nil {
			return nil, err
		}
		uid, err := peerUID(conn)
		if err == nil && uid == os.Getuid() {
			return conn, nil
		}
		conn.Close()
	}
}

type workbenchHTTP struct {
	server   *http.Server
	done     chan struct{}
	once     sync.Once
	cancel   context.CancelFunc
	observed chan struct{}
}

const workbenchWriteTimeout = 40 * time.Second

func listenWorkbenchHTTP(s *services.Service, path, dir string) (*workbenchHTTP, error) {
	l, err := listenLocal(path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := newWorkbenchEvents(ctx)
	handler, _ := workbenchRouterWithEvents(s, events)
	read, release, err := dal.OpenChangeReader(ctx, filepath.Join(dir, "apilot.db"))
	if err != nil {
		cancel()
		l.Close()
		return nil, err
	}
	initial, err := read(ctx)
	if err != nil {
		release()
		cancel()
		l.Close()
		return nil, err
	}
	run := &workbenchHTTP{
		server:   &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: workbenchWriteTimeout, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10},
		done:     make(chan struct{}),
		cancel:   cancel,
		observed: make(chan struct{}),
	}
	volatile := s.VolatileRevision()
	go func() {
		defer close(run.observed)
		defer release()
		events.observe(read, initial, s.VolatileRevision, volatile)
	}()
	go func() {
		defer close(run.done)
		_ = run.server.Serve(workbenchListener{l})
	}()
	return run, nil
}

func (h *workbenchHTTP) close() {
	h.once.Do(func() {
		h.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), workbenchWriteTimeout+5*time.Second)
		defer cancel()
		if err := h.server.Shutdown(ctx); err != nil {
			_ = h.server.Close()
		}
		<-h.observed
		<-h.done
	})
}
