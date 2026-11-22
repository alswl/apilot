//go:build darwin || linux

package server

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

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
	server *http.Server
	done   chan struct{}
	once   sync.Once
}

func listenWorkbenchHTTP(s *services.Service, path string) (*workbenchHTTP, error) {
	l, err := listenLocal(path)
	if err != nil {
		return nil, err
	}
	handler, _ := workbenchRouter(s)
	run := &workbenchHTTP{
		server: &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10},
		done:   make(chan struct{}),
	}
	go func() {
		defer close(run.done)
		_ = run.server.Serve(workbenchListener{l})
	}()
	return run, nil
}

func (h *workbenchHTTP) close() {
	h.once.Do(func() {
		_ = h.server.Shutdown(context.Background())
		<-h.done
	})
}
