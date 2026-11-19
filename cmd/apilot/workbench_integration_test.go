package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/server"
	"github.com/alswl/apilot/pkg/services"
)

func TestWorkbenchReadPath(t *testing.T) {
	dir, vault := t.TempDir(), t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	work := filepath.Join(vault, "Work")
	if err := os.Mkdir(work, 0700); err != nil {
		t.Fatal(err)
	}
	note := filepath.Join(work, "Release.md")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(note, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("# Release\n- [ ] Acceptance 🆔 accept1\n- [ ] Remove\n")
	runtime, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { runtime.Close() }()
	call := func(wantCode int, args ...string) json.RawMessage {
		t.Helper()
		var out, errs bytes.Buffer
		code := execute(append(args, "--json"), &out, &errs)
		if code != wantCode {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &out, &errs)
		}
		var reply struct{ Data json.RawMessage }
		if wantCode == 0 {
			if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
				t.Fatal(err)
			}
		}
		return reply.Data
	}
	get := func(id string) common.WorkbenchPage {
		t.Helper()
		args := []string{"workbench", "task", "list"}
		if id != "" {
			args = append(args, "--connection", id)
		}
		var page common.WorkbenchPage
		if err := json.Unmarshal(call(0, args...), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	if page := get(""); page.Total != 0 {
		t.Fatalf("unexpected default data: %+v", page)
	}
	wait := func(service *services.Service, id, state string, after int64) common.WorkbenchAttempt {
		t.Helper()
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) {
			snapshot, err := service.WorkbenchSnapshot(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Attempts) > 0 {
				a := snapshot.Attempts[0]
				if a.ID > after && a.State == state {
					return a
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		snapshot, _ := service.WorkbenchSnapshot(id)
		t.Fatalf("did not reach %s: %+v", state, snapshot)
		return common.WorkbenchAttempt{}
	}
	obs, err := runtime.Service.SaveWorkbench(common.WorkbenchConnection{Name: "Vault", Vault: vault, Scope: "Work", Kind: common.WorkbenchObsidianTasks})
	if err != nil {
		t.Fatal(err)
	}
	a := wait(runtime.Service, obs, "success", 0)
	before := get(obs)
	if before.Total != 2 {
		t.Fatalf("%+v", before)
	}
	id := before.Items[0].ID
	call(0, "workbench", "task", "show", id)
	call(0, "workbench", "sync", "show", obs)
	call(4, "workbench", "task", "show", "unknown-id")
	call(2, "workbench", "task", "list", "--state", "bad")
	mock, err := runtime.Service.SaveWorkbench(common.WorkbenchConnection{Name: "Mock", Repository: "alswl/issues", Kind: common.WorkbenchGitHubIssues})
	if err != nil {
		t.Fatal(err)
	}
	wait(runtime.Service, mock, "success", 0)
	if page := get(mock); page.Total != 2 || page.Items[0].Mode != "mock" {
		t.Fatalf("%+v", page)
	}
	call(0, "workbench", "connection", "list")
	changed := "# Release\n- [x] Acceptance 🆔 accept1\n- [ ] Added\n"
	write(changed)
	if err := runtime.Service.SyncWorkbench(obs); err != nil {
		t.Fatal(err)
	}
	a = wait(runtime.Service, obs, "success", a.ID)
	after := get(obs)
	if a.Added != 1 || a.Updated != 1 || a.Removed != 1 || after.Items[0].ID != id || after.Items[0].State != "done" {
		t.Fatalf("%+v %+v", a, after)
	}
	if err := runtime.Service.SyncWorkbench(obs); err != nil {
		t.Fatal(err)
	}
	a = wait(runtime.Service, obs, "success", a.ID)
	if a.Added+a.Updated+a.Removed != 0 || get(obs).Revision != after.Revision {
		t.Fatalf("non-idempotent %+v", a)
	}
	if content, err := os.ReadFile(note); err != nil || string(content) != changed {
		t.Fatalf("source changed: %q %v", content, err)
	}
	if err := os.Rename(work, work+".offline"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Service.SyncWorkbench(obs); err != nil {
		t.Fatal(err)
	}
	a = wait(runtime.Service, obs, "failed", a.ID)
	if get(obs).Total != 2 {
		t.Fatal("failure removed snapshot")
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, err = server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	wait(runtime.Service, obs, "failed", a.ID)
	if get(obs).Total != 2 {
		t.Fatal("restart lost snapshot")
	}
	if err := os.Mkdir(filepath.Join(vault, "Empty"), 0700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := runtime.Service.WorkbenchSnapshot(obs)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Scope = "Empty"
	if _, err := runtime.Service.SaveWorkbench(snapshot.WorkbenchConnection); err != nil {
		t.Fatal(err)
	}
	if get(obs).Total != 0 {
		t.Fatal("obsolete scope is visible")
	}
}
