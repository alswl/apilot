package services

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alswl/apilot/pkg/common"
)

func TestWorkbenchObsidianCLIWriteAndReconcile(t *testing.T) {
	t.Run("CLI reads", func(t *testing.T) { testWorkbenchObsidianCLIWrite(t, common.WorkbenchObsidianCLI) })
	t.Run("Markdown reads", func(t *testing.T) { testWorkbenchObsidianCLIWrite(t, "") })
}

func testWorkbenchObsidianCLIWrite(t *testing.T, readMethod string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell CLI fixture")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required to execute the CLI JavaScript fixture")
	}
	vault, bin := t.TempDir(), t.TempDir()
	note := filepath.Join(vault, "Release.md")
	original := "# Release\r\n\r\nExisting notes.\r\n# First\r\n- [ ] Review\r\n# Second\r\n- [ ] Review\r\n"
	if err := os.WriteFile(note, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	otherVault := t.TempDir()
	otherNote := filepath.Join(otherVault, "Release.md")
	if err := os.WriteFile(otherNote, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	fixture := `#!/usr/bin/env node
const fs = require("fs");
const path = require("path");
const [selector, command, argument] = process.argv.slice(2);
const roots = [process.env.APILOT_CLI_TEST_VAULT, process.env.APILOT_CLI_TEST_OTHER_VAULT];
const root = roots.find(v => selector === "vault=" + path.basename(v));
const outcome = process.env.APILOT_CLI_TEST_OUTCOME;
const note = "Release.md";
const notePath = path.join(root, note);
const app = { vault: {
    adapter: { getBasePath: () => root },
    getFileByPath: name => name === note ? { stat: fs.statSync(notePath) } : null,
    cachedRead: () => fs.readFileSync(notePath, "utf8"),
    process: async (_, update) => {
        if (outcome === "competing") fs.appendFileSync(notePath, "Editor text.\r\n");
        const next = update(fs.readFileSync(notePath, "utf8"));
        fs.writeFileSync(notePath, next);
    },
}};
async function run() {
    if (selector !== "vault=" + path.basename(root)) throw new Error("Unexpected vault");
    if (command === "vault" && argument === "info=path") return root;
    if (command === "files") return note;
    if (command === "eval" && argument.startsWith("code=")) {
        const result = await eval(argument.slice(5));
        if (result === "written" && outcome === "lost") throw new Error("Lost CLI reply");
        return "=> " + result;
    }
    throw new Error("Unexpected command");
}
run().then(console.log).catch(error => { console.log("Error: " + error.message); process.exitCode = 1 });
`
	if err := os.WriteFile(filepath.Join(bin, "obsidian"), []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("APILOT_CLI_TEST_VAULT", vault)
	t.Setenv("APILOT_CLI_TEST_OTHER_VAULT", otherVault)
	t.Setenv("APILOT_CLI_TEST_OUTCOME", "")
	s, err := openService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	enabled := true
	id, err := s.ConfigureWorkbench(common.WorkbenchConnection{
		Kind: common.WorkbenchObsidianTasks, Name: "Test vault", Vault: vault, ReadMethod: readMethod,
	}, common.WorkbenchBridgeInput{WriteEnabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot common.WorkbenchSnapshot
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		snapshot, err = s.WorkbenchSnapshot(id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Attempts[0].State == "success" {
			break
		}
		if snapshot.Attempts[0].State != "running" || time.Now().After(deadline) {
			t.Fatalf("initial scan: %+v", snapshot.Attempts)
		}
	}
	if !snapshot.Capabilities.Create || !snapshot.Capabilities.SetState || snapshot.BridgeConfigured {
		t.Fatalf("unexpected CLI write capabilities: %+v", snapshot)
	}
	headings, err := s.WorkbenchHeadings(id, "Release.md")
	if err != nil || len(headings) != 3 {
		t.Fatalf("headings: %+v %v", headings, err)
	}
	input := common.WorkbenchTaskWrite{
		RequestID: "create", ConnectionID: id, ConnectionVersion: snapshot.Version,
		NotePath: "Release.md", HeadingPath: headings[0].HeadingPath, TargetVersion: headings[0].TargetVersion, Title: "发布 🧪",
	}
	t.Setenv("APILOT_CLI_TEST_OUTCOME", "lost")
	created, err := s.WriteWorkbenchTask(input)
	if err != nil || created.SyncState != "confirmed" {
		t.Fatalf("write with a lost reply: %+v %v", created, err)
	}
	if replayed, err := s.WriteWorkbenchTask(input); err != nil || replayed != created {
		t.Fatalf("create replay: %+v %v", replayed, err)
	}
	readNote := func() string {
		t.Helper()
		data, err := os.ReadFile(note)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	beforeComplete := readNote()
	if strings.Count(beforeComplete, "- [ ] 发布 🧪") != 1 || !strings.Contains(beforeComplete, "Existing notes.\r\n") {
		t.Fatal("write lost text or inserted duplicate tasks")
	}
	task, err := s.WorkbenchTask(created.SourceObjectID)
	if err != nil {
		t.Fatal(err)
	}
	complete := common.WorkbenchTaskWrite{
		RequestID: "complete", ConnectionVersion: snapshot.Version, SourceObjectID: task.ID, TargetVersion: task.TargetVersion, State: "done",
	}
	t.Setenv("APILOT_CLI_TEST_OUTCOME", "competing")
	if receipt, err := s.WriteWorkbenchTask(complete); err != nil || receipt.SyncState != "conflict" {
		t.Fatalf("competing editor write: %+v %v", receipt, err)
	}
	if readNote() != beforeComplete+"Editor text.\r\n" {
		t.Fatal("the CLI write overwrote a concurrent edit")
	}
	t.Setenv("APILOT_CLI_TEST_OUTCOME", "")
	complete.RequestID = "complete-again"
	if receipt, err := s.WriteWorkbenchTask(complete); err != nil || receipt.SyncState != "confirmed" {
		t.Fatalf("complete: %+v %v", receipt, err)
	}
	if readNote() != strings.Replace(beforeComplete, "- [ ] 发布", "- [x] 发布", 1)+"Editor text.\r\n" {
		t.Fatal("completion changed bytes outside the checkbox")
	}
	task, err = s.WorkbenchTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	complete.RequestID, complete.TargetVersion, complete.State = "reopen", task.TargetVersion, "open"
	if receipt, err := s.WriteWorkbenchTask(complete); err != nil || receipt.SyncState != "confirmed" {
		t.Fatalf("reopen: %+v %v", receipt, err)
	}
	if readNote() != beforeComplete+"Editor text.\r\n" {
		t.Fatal("reopen changed bytes outside the checkbox")
	}
	otherID, err := s.ConfigureWorkbench(common.WorkbenchConnection{
		Kind: common.WorkbenchObsidianTasks, Name: "Second vault", Vault: otherVault, ReadMethod: readMethod,
	}, common.WorkbenchBridgeInput{WriteEnabled: &enabled})
	if err != nil || otherID == id {
		t.Fatalf("second vault: %s %v", otherID, err)
	}
	page, err := s.WorkbenchTasks(common.WorkbenchQuery{Connection: id})
	if err != nil {
		t.Fatal(err)
	}
	var second common.WorkbenchTask
	for _, task := range page.Items {
		if task.Title == "Review" && task.NativeFields["section"] == "Second" {
			second = task
		}
	}
	if second.ID == "" {
		t.Fatal("could not find repeated task in the second section")
	}
	receipt, err := s.WriteWorkbenchTask(common.WorkbenchTaskWrite{
		RequestID: "complete-second-review", SourceObjectID: second.ID,
		ConnectionVersion: second.ConnectionVersion, TargetVersion: second.TargetVersion, State: "done",
	})
	if err != nil || receipt.SyncState != "confirmed" || receipt.SourceObjectID != second.ID {
		t.Fatalf("complete second review: %+v %v", receipt, err)
	}
	if text := readNote(); !strings.Contains(text, "# First\r\n- [ ] Review\r\n# Second\r\n- [x] Review") {
		t.Fatal("completing the second Review changed a different task")
	}
	if data, err := os.ReadFile(otherNote); err != nil || string(data) != original {
		t.Fatal("the second vault should remain unchanged")
	}
}
