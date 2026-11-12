package host

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/model"
	"github.com/alswl/apilot/internal/runner"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		os.Exit(runner.Serve())
	}
	os.Exit(m.Run())
}
func TestOpenRollbackAndClose(t *testing.T) {
	dir := t.TempDir()
	path := app.SocketPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not socket"), 0600); err != nil {
		t.Fatal(err)
	}
	if r, e := Open(dir); e == nil {
		r.Close()
		t.Fatal("unsafe socket accepted")
	}
	os.Remove(path)
	r, e := Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	r.StopAdmission()
	if _, e = r.Service.SaveTask("", model.TaskInput{}, 0); e == nil {
		t.Fatal("stopping admitted mutation")
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := r.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	r, e = Open(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	if _, e = os.Stat(filepath.Join(dir, "apilot.db")); e != nil {
		t.Fatal(e)
	}
}
func TestCloseWithIdleIPCConnection(t *testing.T) {
	r, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	c, e := net.Dial("unix", r.SocketPath)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.Write([]byte("{"))
	time.Sleep(20 * time.Millisecond)
	started := time.Now()
	if e = r.Close(); e != nil {
		t.Fatal(e)
	}
	if time.Since(started) > time.Second {
		t.Fatal("idle IPC delayed shutdown")
	}
}
