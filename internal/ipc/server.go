package ipc

import (
	"bufio"
	"encoding/json"
	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/model"
	"golang.org/x/sys/unix"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Server struct {
	listener *net.UnixListener
	wg       sync.WaitGroup
	done     chan struct{}
	path     string
}

func Listen(s *app.Service, path string) (*Server, error) {
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
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Getuid() || st.Mode()&os.ModeSocket == 0 {
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
	srv := &Server{listener: l, done: make(chan struct{}), path: path}
	slots := make(chan struct{}, 32)
	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		for {
			c, e := l.AcceptUnix()
			if e != nil {
				return
			}
			select {
			case slots <- struct{}{}:
				srv.wg.Add(1)
				go func() { defer srv.wg.Done(); defer func() { <-slots }(); defer c.Close(); handle(s, c) }()
			default:
				c.Close()
			}
		}
	}()
	return srv, nil
}
func handle(s *app.Service, c *net.UnixConn) {
	raw, e := c.SyscallConn()
	if e != nil {
		return
	}
	uid := -1
	_ = raw.Control(func(fd uintptr) {
		u, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err == nil {
			uid = int(u.Uid)
		}
	})
	if uid != os.Getuid() {
		return
	}
	c.SetDeadline(time.Now().Add(2 * time.Second))
	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	if !scanner.Scan() {
		if scanner.Err() != nil {
			_ = json.NewEncoder(c).Encode(Response{Version: 1, Error: model.Err("INVALID_ARGUMENT", "Request exceeds the size limit or read deadline")})
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
func (s *Server) Close() { s.listener.Close(); s.wg.Wait() }
