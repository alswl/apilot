package integrations

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		os.Exit(Serve())
	}
	if len(os.Args) > 2 && os.Args[1] == "--test-host" {
		fixture, err := filepath.Abs("../../scripts/fixtures/process_tree.sh")
		if err != nil {
			panic(err)
		}
		p, err := Start(context.Background(), Spec{Executable: "/bin/sh", Args: []string{fixture, os.Args[2]}, Cwd: "/tmp", TimeoutMS: 120000}, filepath.Dir(os.Args[2]))
		if err != nil {
			panic(err)
		}
		_ = p
		select {}
	}
	os.Exit(m.Run())
}
func spec(command string) Spec {
	return Spec{Executable: "/bin/sh", Args: []string{"-c", command}, Cwd: "/tmp", TimeoutMS: 5000}
}
func waitResult(t *testing.T, p *Process) Result {
	t.Helper()
	select {
	case r := <-p.Done:
		return r
	case <-time.After(10 * time.Second):
		p.Stop()
		t.Fatal("runner did not finish")
		return Result{}
	}
}
func TestExecute(t *testing.T) {
	dir := t.TempDir()
	p, e := Start(context.Background(), spec("printf hello; printf error >&2; exit 7"), dir)
	if e != nil {
		t.Fatal(e)
	}
	r := waitResult(t, p)
	if r.Status != "failed" || r.ExitCode == nil || *r.ExitCode != 7 {
		t.Fatalf("%+v", r)
	}
	for f, want := range map[string]string{"stdout.log": "hello", "stderr.log": "error"} {
		b, e := os.ReadFile(filepath.Join(dir, f))
		if e != nil || string(b) != want {
			t.Fatalf("%s: %q %v", f, b, e)
		}
	}
}
func TestTimeoutAndCancel(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s := spec("sleep 120 & wait")
			if mode == "timeout" {
				s.TimeoutMS = 150
			}
			p, e := Start(context.Background(), s, t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			if mode == "cancel" {
				p.Stop()
			}
			r := waitResult(t, p)
			want := "canceled"
			if mode == "timeout" {
				want = "failed"
				if r.ErrorCode != "TIMEOUT" {
					t.Fatalf("%+v", r)
				}
			}
			if r.Status != want {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestStartFailure(t *testing.T) {
	s := spec("")
	s.Executable = "/nonexistent/apilot-fixture"
	p, e := Start(context.Background(), s, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	r := waitResult(t, p)
	if r.Status != "failed" || r.ExitCode != nil {
		t.Fatalf("%+v", r)
	}
}
func TestStructuredArgs(t *testing.T) {
	dir := t.TempDir()
	s := spec("")
	s.Executable = "/bin/echo"
	s.Args = []string{"$(touch /tmp/apilot-must-not-execute)", "a b"}
	p, e := Start(context.Background(), s, dir)
	if e != nil {
		t.Fatal(e)
	}
	if r := waitResult(t, p); r.Status != "succeeded" {
		t.Fatal(r)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "stdout.log"))
	if string(b) != "$(touch /tmp/apilot-must-not-execute) a b\n" {
		t.Fatal(string(b))
	}
}
func TestAppCrashCleansGroup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pids")
	exe, _ := os.Executable()
	host := exec.Command(exe, "--test-host", file)
	host.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	host.Stderr = os.Stderr
	if e := host.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = host.Process.Kill(); _ = host.Wait() }()
	var pids []int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(file)
		v := strings.Fields(string(b))
		if len(v) == 2 {
			for _, x := range v {
				n, _ := strconv.Atoi(x)
				pids = append(pids, n)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(pids) != 2 {
		t.Fatal("fixture not ready")
	}
	defer func() {
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}()
	_ = syscall.Kill(-host.Process.Pid, syscall.SIGKILL)
	_ = host.Wait()
	for _, pid := range pids {
		deadline = time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) && syscall.Kill(pid, 0) == nil {
			time.Sleep(30 * time.Millisecond)
		}
		if syscall.Kill(pid, 0) == nil {
			t.Errorf("orphan pid %d", pid)
		}
	}
}
func TestEOFDuringHandshake(t *testing.T) {
	ctlR, ctlW, _ := os.Pipe()
	resR, resW, _ := os.Pipe()
	defer ctlR.Close()
	defer resR.Close()
	exe, _ := os.Executable()
	cmd := exec.Command(exe, "--apilot-runner")
	cmd.ExtraFiles = []*os.File{ctlR, resW}
	cmd.Stderr = os.Stderr
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	resW.Close()
	ctlR.Close()
	var ev Event
	if e := json.NewDecoder(resR).Decode(&ev); e != nil || ev.Type != "ready" {
		t.Fatalf("%+v %v", ev, e)
	}
	ctlW.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cmd.Process.Kill()
		t.Fatal("helper remained after EOF")
	}
}
func TestParentExitCleansBackgroundChild(t *testing.T) {
	file := filepath.Join(t.TempDir(), "child")
	s := spec("sleep 120 & echo $! > \"$1\"")
	s.Args = append(s.Args, "fixture", file)
	p, e := Start(context.Background(), s, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	r := waitResult(t, p)
	if r.Status != "succeeded" {
		t.Fatal(r)
	}
	b, _ := os.ReadFile(file)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid < 1 {
		t.Fatal("missing child")
	}
	if syscall.Kill(pid, 0) == nil {
		syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("background child survived %d", pid)
	}
}

func TestLogLimit(t *testing.T) {
	dir := t.TempDir()
	s := spec("printf 12345678901234567890; printf abcdefghijklmnop >&2")
	s.LogLimit = 10
	p, err := Start(context.Background(), s, dir)
	if err != nil {
		t.Fatal(err)
	}
	r := waitResult(t, p)
	if r.Status != "succeeded" || !r.Truncated {
		t.Fatal(r)
	}
	for _, name := range []string{"stdout.log", "stderr.log"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(b) != 10 {
			t.Fatalf("%s: %d %v", name, len(b), err)
		}
	}
}
func TestCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, err := Start(ctx, spec("echo should-not-run"), t.TempDir()); err == nil {
		p.Stop()
		t.Fatal("canceled request launched")
	}
}
