package integrations

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/alswl/apilot/pkg/common"
)

func TestWorkbenchObsidianCLIReadsScopedMarkdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell CLI fixture")
	}
	vault := t.TempDir()
	scope := filepath.Join(vault, "Work")
	if err := os.Mkdir(scope, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("# Heading\n\n- [ ] Shared task\n")
	if err := os.WriteFile(filepath.Join(scope, "a note.md"), content, 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	cli := `#!/bin/sh
[ "$1" = "vault=${APILOT_OBSIDIAN_TEST_VAULT##*/}" ] || exit 1
case "$2:$3" in
  vault:info=path) printf '%s\n' "$APILOT_OBSIDIAN_TEST_VAULT";;
  files:) printf '%s\n' 'Work/a note.md' 'Work/asset.png' 'Outside.md';;
  eval:code=*)
    if [ "$APILOT_OBSIDIAN_TEST_OFFLINE" = 1 ]; then
      printf '%s\n' 'The CLI is unable to find Obsidian.' >&2
      exit 1
    else
      printf '%s\n' '[warn] Background plugin console message' '=> apilot-note-batch-v1:["# Heading\n\n- [ ] Shared task\n"]'
    fi;;
  *) exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "obsidian"), []byte(cli), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("APILOT_OBSIDIAN_TEST_VAULT", vault)
	t.Setenv("APILOT_OBSIDIAN_TEST_OFFLINE", "0")
	connection := common.WorkbenchConnection{Vault: vault, Scope: "Work"}
	direct, err := ReadWorkbenchObsidian(t.Context(), connection)
	if err != nil || len(direct) != 1 {
		t.Fatalf("direct scan: %v %v", direct, err)
	}
	connection.ReadMethod = common.WorkbenchObsidianCLI
	actual, err := ReadWorkbenchObsidian(t.Context(), connection)
	if err != nil || !reflect.DeepEqual(actual, direct) {
		t.Fatalf("CLI scan differs from direct scan: %v %v", actual, err)
	}
	t.Setenv("APILOT_OBSIDIAN_TEST_OFFLINE", "1")
	if _, err := ReadWorkbenchObsidian(t.Context(), connection); err == nil || common.ProtocolError(err).Code != "IPC_UNAVAILABLE" {
		t.Fatalf("expected a CLI connection failure, got %v", err)
	}
}

func TestWorkbenchObsidianCLISplitsIncompleteBatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a shell CLI fixture")
	}
	vault, bin := t.TempDir(), t.TempDir()
	fixture := `#!/bin/sh
case "$2:$3" in
  vault:info=path) printf '%s\n' "$APILOT_OBSIDIAN_BATCH_VAULT";;
  files:) printf '%s\n' 'First.md' 'Second.md';;
  eval:code=*First.md*Second.md*) printf '%s\n' '=> apilot-note-batch-v1:["truncated';;
  eval:code=*First.md*) printf '%s\n' '[warn] Ignored console line' '=> apilot-note-batch-v1:["- [ ] First task\n"]';;
  eval:code=*Second.md*) printf '%s\n' '=> apilot-note-batch-v1:["- [ ] Second task\n"]';;
  *) exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "obsidian"), []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("APILOT_OBSIDIAN_BATCH_VAULT", vault)
	c := common.WorkbenchConnection{Vault: vault, Scope: ".", ReadMethod: common.WorkbenchObsidianCLI}
	tasks, err := ReadWorkbenchObsidian(t.Context(), c)
	if err != nil || len(tasks) != 2 || tasks[0].Title != "First task" || tasks[1].Title != "Second task" {
		t.Fatalf("split scan: %+v %v", tasks, err)
	}
}
