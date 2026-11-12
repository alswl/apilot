//go:build darwin || linux

package server

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/services"
	"golang.org/x/sys/unix"
)

type Server struct {
	listener    *net.UnixListener
	mu          sync.Mutex
	closeOnce   sync.Once
	closing     bool
	connections map[*net.UnixConn]struct{}
	wg          sync.WaitGroup
	done        chan struct{}
	path        string
}

func Listen(s *services.Service, path string) (*Server, error) {
	if len(path) >= 104 {
		return nil, unix.ENAMETOOLONG
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	if e := os.Chmod(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	if st, e := os.Lstat(path); e == nil {
		if !socketOwnedByCurrentUser(st) || st.Mode()&os.ModeSocket == 0 {
			return nil, unix.EPERM
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
	} else if !os.IsNotExist(e) {
		return nil, e
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		l.Close()
		return nil, e
	}
	srv := &Server{listener: l, done: make(chan struct{}), path: path, connections: make(map[*net.UnixConn]struct{})}
	slots := make(chan struct{}, 32)
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		for {
			c, e := l.AcceptUnix()
			if e != nil {
				return
			}
			srv.mu.Lock()
			if srv.closing {
				srv.mu.Unlock()
				c.Close()
				return
			}
			srv.connections[c] = struct{}{}
			srv.mu.Unlock()
			select {
			case slots <- struct{}{}:
				srv.wg.Add(1)
				go func() {
					defer srv.wg.Done()
					defer func() { <-slots }()
					defer func() { c.Close(); srv.mu.Lock(); delete(srv.connections, c); srv.mu.Unlock() }()
					handle(s, c)
				}()
			default:
				c.Close()
				srv.mu.Lock()
				delete(srv.connections, c)
				srv.mu.Unlock()
			}
		}
	}()
	return srv, nil
}
func handle(s *services.Service, c *net.UnixConn) {
	uid, e := peerUID(c)
	if e != nil || uid != os.Getuid() {
		return
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() {
		if scanner.Err() != nil {
			_ = json.NewEncoder(c).Encode(Response{Version: 1, Error: common.Err("INVALID_ARGUMENT", "Request exceeds the size limit or read deadline")})
		}
		return
	}
	var req Request
	resp := Response{Version: Version}
	if e = decode(scanner.Bytes(), &req); e != nil {
		resp.Error = Error(e)
	} else {
		resp.ID = req.ID
		resp.Data, e = Dispatch(s, req)
		if e != nil {
			resp.Data = nil
			resp.Error = Error(e)
		}
	}
	_ = json.NewEncoder(c).Encode(resp)
}
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		s.listener.Close()
		for c := range s.connections {
			c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
}
