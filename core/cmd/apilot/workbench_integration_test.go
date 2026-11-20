package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/server"
	"github.com/alswl/apilot/core/pkg/services"
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
	file := filepath.Join(t.TempDir(), "input.json")
	send := func(input string, code int, args ...string) json.RawMessage {
		t.Helper()
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		return call(code, append(args, file)...)
	}
	var created common.WorkbenchSnapshot
	data := send(`{"kind":"github_issues","name":"CLI mock","repository":"alswl/issues"}`, 0, "workbench", "connection", "save")
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	previous := wait(runtime.Service, created.ID, "success", 0)
	call(0, "workbench", "connection", "show", created.ID)
	call(0, "workbench", "sync", "run", created.ID)
	wait(runtime.Service, created.ID, "success", previous.ID)
	call(0, "workbench", "connection", "remove", created.ID)
	call(4, "workbench", "connection", "show", created.ID)
	data = send(`{"name":"Collected","tasks":[{"key":"a","title":"Original","state":"open"}]}`, 0, "workbench", "task", "import")
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatal(err)
	}
	page := get(created.ID)
	if page.Total != 1 || page.Items[0].Kind != common.WorkbenchLocal || page.Items[0].Mode != "local" {
		t.Fatalf("%+v", page)
	}
	importedID := page.Items[0].ID
	send(`{"name":"Collected","tasks":[{"key":"a","title":"Updated","state":"done"}]}`, 0, "workbench", "task", "import")
	page = get(created.ID)
	if page.Total != 1 || page.Items[0].ID != importedID || page.Items[0].State != "done" {
		t.Fatalf("%+v", page)
	}
	send(`{"name":"Collected","tasks":[{"key":"a","title":"Invalid","state":"invalid"}]}`, 2, "workbench", "task", "import")
	if get(created.ID).Items[0].Title != "Updated" {
		t.Fatal("invalid import overwrote the snapshot")
	}
	send(`{"name":"Collected","tasks":[]}`, 0, "workbench", "task", "import")
	if get(created.ID).Total != 0 {
		t.Fatal("empty import did not clear the collection")
	}

	var catalog struct {
		Items []struct {
			Kind string `json:"kind"`
		}
	}
	if err := json.Unmarshal(call(0, "workbench", "connector", "list"), &catalog); err != nil {
		t.Fatal(err)
	}
	foundLocal := false
	for _, item := range catalog.Items {
		foundLocal = foundLocal || item.Kind == common.WorkbenchLocal
	}
	if !foundLocal {
		t.Fatal("connector catalog omitted Local")
	}
	call(0, "workbench", "connection", "show", common.DefaultWorkbenchLocalID)
	send(`{"tasks":[{"key":"hello","title":"Default Local task","state":"open"}]}`, 0, "workbench", "task", "import")
	if page := get(common.DefaultWorkbenchLocalID); page.Total != 1 || page.Items[0].Kind != common.WorkbenchLocal {
		t.Fatalf("%+v", page)
	}
	call(2, "workbench", "sync", "run", common.DefaultWorkbenchLocalID)
	call(5, "workbench", "connection", "remove", common.DefaultWorkbenchLocalID)

}

func TestWorkbenchLocalTaskCommandsAcrossServiceRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	runtime, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close() }()
	call := func(want int, result any, args ...string) {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := execute(append([]string{"workbench"}, append(args, "--json")...), &out, &stderr); code != want {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &out, &stderr)
		}
		if result != nil {
			envelope := struct{ Data any }{Data: result}
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
		}
	}
	file := filepath.Join(t.TempDir(), "task.json")
	write := func(input common.WorkbenchTaskWrite) {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var plugins struct{ Items []services.WorkbenchPlugin }
	call(0, &plugins, "plugin", "list")
	localEnabled := false
	for _, plugin := range plugins.Items {
		if plugin.ID == common.WorkbenchLocal {
			localEnabled = plugin.Enabled
		}
	}
	if !localEnabled {
		t.Fatal("Local plugin should be present and enabled by default")
	}
	create := common.WorkbenchTaskWrite{RequestID: "create-1", ConnectionID: common.DefaultWorkbenchLocalID, ConnectionVersion: 1, Title: "CLI creates it"}
	write(create)
	var created common.WorkbenchTaskReceipt
	call(0, &created, "task", "create", file)
	if created.SyncState != "confirmed" || created.SourceObjectID == "" {
		t.Fatalf("create: %+v", created)
	}
	call(0, nil, "plugin", "disable", common.WorkbenchLocal)
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := server.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	runtime = restarted
	write(common.WorkbenchTaskWrite{RequestID: "disabled-write", ConnectionID: common.DefaultWorkbenchLocalID, ConnectionVersion: 1, Title: "Must not create"})
	call(5, nil, "task", "create", file)
	call(0, nil, "plugin", "enable", common.WorkbenchLocal)
	write(create)
	var replay common.WorkbenchTaskReceipt
	call(0, &replay, "task", "create", file)
	if replay != created {
		t.Fatalf("replay: %+v, original: %+v", replay, created)
	}
	for _, action := range []struct{ verb, state string }{{"complete", "done"}, {"reopen", "open"}} {
		var before, after common.WorkbenchTask
		call(0, &before, "task", "show", created.SourceObjectID)
		write(common.WorkbenchTaskWrite{RequestID: action.verb, SourceObjectID: before.ID, ConnectionVersion: before.ConnectionVersion, TargetVersion: before.TargetVersion})
		var receipt common.WorkbenchTaskReceipt
		call(0, &receipt, "task", action.verb, file)
		call(0, &after, "task", "show", created.SourceObjectID)
		if receipt.SyncState != "confirmed" || after.State != action.state || after.ID != before.ID {
			t.Fatalf("%s: %+v %+v", action.verb, receipt, after)
		}
	}
	var page common.WorkbenchPage
	call(0, &page, "task", "list", "--connection", common.DefaultWorkbenchLocalID)
	if page.Total != 1 {
		t.Fatalf("Local writes produced %d tasks", page.Total)
	}
}
