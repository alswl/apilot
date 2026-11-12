//go:build darwin

package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Spec struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Cwd        string   `json:"cwd"`
	Env        []string `json:"env"`
	TimeoutMS  int64    `json:"timeout_ms"`
	LogLimit   int64    `json:"log_limit"`
}
type Result struct {
	Status    string `json:"status"`
	ExitCode  *int   `json:"exit_code"`
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated"`
}
type Event struct {
	Type   string  `json:"type"`
	PID    int     `json:"pid,omitempty"`
	Result *Result `json:"result,omitempty"`
}
type Process struct {
	Done <-chan Result
	PID  int
	stop func()
}

func (p *Process) Stop() { p.stop() }

func Start(ctx context.Context, s Spec, dir string) (*Process, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.TimeoutMS <= 0 {
		return nil, fmt.Errorf("timeout must be positive")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	var files []*os.File
	for _, name := range []string{"stdout.log", "stderr.log"} {
		f, e := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if e != nil {
			for _, x := range files {
				x.Close()
			}
			return nil, e
		}
		files = append(files, f)
	}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	ctlR, ctlW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer ctlR.Close()
	resR, resW, err := os.Pipe()
	if err != nil {
		ctlW.Close()
		return nil, err
	}
	defer resW.Close()
	exe, err := os.Executable()
	if err != nil {
		ctlW.Close()
		resR.Close()
		return nil, err
	}
	cmd := exec.Command(exe, "--apilot-runner")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = []*os.File{ctlR, resW, files[0], files[1]}
	if err = cmd.Start(); err != nil {
		ctlW.Close()
		resR.Close()
		return nil, err
	}
	resW.Close()
	ctlR.Close()
	var once sync.Once
	stop := func() { once.Do(func() { ctlW.Close() }) }
	fail := func(e error) (*Process, error) {
		stop()
		waited := make(chan error, 1)
		go func() { waited <- cmd.Wait() }()
		select {
		case <-waited:
		case <-time.After(4 * time.Second):
			_ = cmd.Process.Kill()
			<-waited
		}
		resR.Close()
		return nil, e
	}
	// Handshake deadlines also cover a helper that cannot initialize.
	_ = resR.SetReadDeadline(time.Now().Add(2 * time.Second))
	dec := json.NewDecoder(resR)
	var ready Event
	if err = dec.Decode(&ready); err != nil || ready.Type != "ready" {
		return fail(fmt.Errorf("runner handshake failed: %v", err))
	}
	if err = json.NewEncoder(ctlW).Encode(s); err != nil {
		return fail(err)
	}
	var started Event
	if err = dec.Decode(&started); err != nil {
		return fail(err)
	}
	_ = resR.SetReadDeadline(time.Time{})
	done := make(chan Result, 1)
	p := &Process{Done: done, PID: started.PID, stop: stop}
	go func() {
		defer resR.Close()
		defer stop()
		defer close(done)
		finished := make(chan struct{})
		defer close(finished)
		go func() {
			select {
			case <-ctx.Done():
				stop()
			case <-finished:
			}
		}()
		event := started
		if event.Type != "result" {
			if err := dec.Decode(&event); err != nil {
				event = Event{Result: &Result{Status: "interrupted", ErrorCode: "RUNNER_LOST", Error: "Execution helper ended without a result"}}
			}
		}
		_ = cmd.Wait()
		if event.Result == nil {
			event.Result = &Result{Status: "interrupted", ErrorCode: "RUNNER_LOST"}
		}
		done <- *event.Result
	}()
	return p, nil
}

func Serve() int {
	ctl := os.NewFile(3, "control")
	result := os.NewFile(4, "result")
	if ctl == nil || result == nil {
		return 2
	}
	defer ctl.Close()
	defer result.Close()
	syscall.CloseOnExec(int(ctl.Fd()))
	syscall.CloseOnExec(int(result.Fd()))
	enc := json.NewEncoder(result)
	if enc.Encode(Event{Type: "ready"}) != nil {
		return 2
	}
	dec := json.NewDecoder(ctl)
	var s Spec
	if dec.Decode(&s) != nil {
		return 0
	}
	if s.TimeoutMS <= 0 {
		return 2
	}
	gone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, ctl); close(gone) }()
	select {
	case <-gone:
		return 0
	default:
	}
	stdout := os.NewFile(5, "stdout")
	stderr := os.NewFile(6, "stderr")
	if stdout == nil || stderr == nil {
		return 2
	}
	defer stdout.Close()
	defer stderr.Close()
	if s.LogLimit <= 0 {
		s.LogLimit = 50 * 1024 * 1024
	}
	out := &limitedLog{file: stdout, remaining: s.LogLimit}
	errout := &limitedLog{file: stderr, remaining: s.LogLimit}
	cmd := exec.Command(s.Executable, s.Args...)
	cmd.Dir = s.Cwd
	cmd.Env = s.Env
	cmd.Stdout = out
	cmd.Stderr = errout
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	send := func(r Result) {
		r.Truncated = out.truncated || errout.truncated
		_ = enc.Encode(Event{Type: "result", Result: &r})
	}
	if err := cmd.Start(); err != nil {
		send(Result{Status: "failed", ErrorCode: "START_FAILED", Error: err.Error()})
		return 0
	}
	_ = enc.Encode(Event{Type: "started", PID: cmd.Process.Pid})
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	timer := time.NewTimer(time.Duration(s.TimeoutMS) * time.Millisecond)
	defer timer.Stop()
	r := Result{Status: "succeeded"}
	var waitErr error
	select {
	case waitErr = <-waited:
	case <-gone:
		r.Status = "canceled"
		r.ErrorCode = "CANCELED"
		terminateGroup(cmd.Process.Pid)
		waitErr = <-waited
	case <-timer.C:
		r.Status = "failed"
		r.ErrorCode = "TIMEOUT"
		r.Error = "Task exceeded its timeout"
		terminateGroup(cmd.Process.Pid)
		waitErr = <-waited
	}
	// A successful shell may leave group members behind, including pipe writers.
	terminateGroup(cmd.Process.Pid)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		n := cmd.ProcessState.ExitCode()
		r.ExitCode = &n
	}
	if r.Status == "succeeded" && waitErr != nil && !errors.Is(waitErr, exec.ErrWaitDelay) {
		r.Status = "failed"
		r.ErrorCode = "COMMAND_FAILED"
		r.Error = waitErr.Error()
	}
	send(r)
	return 0
}
func terminateGroup(pid int) {
	if syscall.Kill(-pid, syscall.SIGTERM) == syscall.ESRCH {
		return
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pid, 0) == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

type limitedLog struct {
	file      *os.File
	remaining int64
	truncated bool
}

func (w *limitedLog) Write(b []byte) (int, error) {
	n := len(b)
	keep := int64(n)
	if keep > w.remaining {
		keep = w.remaining
		w.truncated = true
	}
	if keep > 0 {
		if _, err := w.file.Write(b[:keep]); err != nil {
			return 0, err
		}
		w.remaining -= keep
	}
	return n, nil
}
