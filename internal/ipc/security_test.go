package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/alswl/apilot/internal/app"
	"github.com/alswl/apilot/internal/model"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSocketPermissionsAndRedaction(t *testing.T) {
	s, e := app.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	dir := t.TempDir()
	path := app.SocketPath(dir)
	server, e := Listen(s, path)
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	st, e := os.Stat(path)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal(st, e)
	}
	_, e = s.SaveTask("", model.TaskInput{Name: "secret", Executable: "/bin/true", Cwd: "/tmp", EnvSet: map[string]string{"SECRET": "must-stay-private"}, TimeoutMS: 1000, Enabled: true}, 0)
	if e != nil {
		t.Fatal(e)
	}
	b, e := Call(path, "task.list", nil)
	if e != nil {
		t.Fatal(e)
	}
	var data any
	if e = json.Unmarshal(b, &data); e != nil {
		t.Fatal(e)
	}
	for _, value := range []string{"must-stay-private"} {
		if bytes.Contains(b, []byte(value)) {
			t.Fatal("secret leaked")
		}
	}
	c, e := net.Dial("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	c.Write([]byte(`{"protocol_version":1,"id":"x","method":"status","params":{"unknown":true}}` + "\n"))
	line, e := bufio.NewReader(c).ReadBytes('\n')
	if e != nil {
		t.Fatal(e)
	}
	var response Response
	if e = json.Unmarshal(line, &response); e != nil || response.Error == nil || response.Error.Code != "INVALID_ARGUMENT" {
		t.Fatal(string(line), e)
	}
}

func TestRequestSizeAndDeadline(t *testing.T) {
	s, e := app.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	path := app.SocketPath(t.TempDir())
	server, e := Listen(s, path)
	if e != nil {
		t.Fatal(e)
	}
	defer server.Close()
	c, e := net.Dial("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	c.SetDeadline(time.Now().Add(4 * time.Second))
	c.Write([]byte(strings.Repeat("x", 1024*1024+1) + "\n"))
	line, e := bufio.NewReader(c).ReadBytes('\n')
	c.Close()
	if e != nil {
		t.Fatal(e)
	}
	var r Response
	if e = json.Unmarshal(line, &r); e != nil || r.Error == nil || r.Error.Code != "INVALID_ARGUMENT" {
		t.Fatal(string(line), e)
	}
	c, e = net.Dial("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(4 * time.Second))
	started := time.Now()
	_, e = bufio.NewReader(c).ReadBytes('\n')
	if time.Since(started) > 3*time.Second {
		t.Fatal("server did not enforce read deadline")
	}
}
