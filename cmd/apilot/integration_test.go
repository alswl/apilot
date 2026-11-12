package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/config"
	"github.com/alswl/apilot/pkg/integrations"
	"github.com/alswl/apilot/pkg/server"
	"github.com/alswl/apilot/pkg/services"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--apilot-runner" {
		os.Exit(integrations.Serve())
	}
	os.Exit(m.Run())
}
func TestBinaryClosedLoop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	s, e := server.OpenService(dir)
	if e != nil {
		t.Fatal(e)
	}
	endpoint, e := server.Listen(s, config.SocketPath(dir))
	if e != nil {
		t.Fatal(e)
	}
	defer func() { endpoint.Close(); s.Close() }()
	bin := filepath.Join(t.TempDir(), "apilot")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = os.Environ()
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(string(b), e)
	}
	call := func(args ...string) json.RawMessage {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"--json"}, args...)...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("%v: %s %v", args, b, e)
		}
		var r struct {
			Data  json.RawMessage `json:"data"`
			Error any             `json:"error"`
		}
		if e = json.Unmarshal(b, &r); e != nil || r.Error != nil {
			t.Fatalf("%s %v", b, e)
		}
		return r.Data
	}
	call("status")
	call("task", "add", "codex-review", "--disabled", "--", "codex", "exec", "Review today changes")
	call("task", "show", "codex-review")
	call("task", "remove", "codex-review")
	call("observe", "add", "poll", "--disabled", "--interval", "30s", "--", "/bin/echo", "ok")
	call("observe", "show", "poll")
	call("observe", "list")
	call("observe", "update", "poll", "--timeout", "2s")
	call("observe", "enable", "poll")
	call("observe", "disable", "poll")
	call("observe", "remove", "poll")
	call("settings", "update", "--notify-success=false")
	b := call("task", "add", "smoke", "--interval", "30s", "--", "/bin/sh", "-c", "printf diagnostic >&2; exit 7")
	var task struct {
		ID string `json:"id"`
	}
	json.Unmarshal(b, &task)
	if task.ID == "" {
		t.Fatal("missing ID")
	}
	call("task", "list")
	b = call("task", "run", "smoke")
	var run struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	json.Unmarshal(b, &run)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b = call("run", "show", run.ID)
		json.Unmarshal(b, &run)
		if run.Status == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if run.Status != "failed" {
		t.Fatal(string(b))
	}
	var chunk services.LogChunk
	json.Unmarshal(call("logs", run.ID, "--stream", "stderr"), &chunk)
	if string(chunk.Data) != "diagnostic" {
		t.Fatal(chunk)
	}
	call("task", "update", "smoke", "--", "/bin/echo", "recovered")
	call("task", "disable", "smoke")
	call("task", "remove", "smoke")
	call("run", "show", run.ID)
}
func TestOfflineErrorsAndHelp(t *testing.T) {
	t.Setenv("APILOT_DATA_DIR", t.TempDir())
	var out, errout bytes.Buffer
	if code := execute([]string{"status", "--json"}, &out, &errout); code != 3 {
		t.Fatal(code, out.String(), errout.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("APP_NOT_RUNNING")) {
		t.Fatal(out.String())
	}
	out.Reset()
	if code := execute([]string{"help"}, &out, &errout); code != 0 || !bytes.Contains(out.Bytes(), []byte("task")) {
		t.Fatal(code, out.String())
	}
	out.Reset()
	if code := execute([]string{"task", "add", "--unknown", "--json"}, &out, &errout); code != 2 || !json.Valid(out.Bytes()) {
		t.Fatal(code, out.String())
	}
}
