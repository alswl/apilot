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
  'read:path=Work/a note.md')
    if [ "$APILOT_OBSIDIAN_TEST_OFFLINE" = 1 ]; then
      printf '%s\n' 'The CLI is unable to find Obsidian.'
    else
      printf '%s\n' '# Heading' '' '- [ ] Shared task'
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
	if _, err := ReadWorkbenchObsidian(t.Context(), connection); err == nil {
		t.Fatal("CLI failure silently fell back to direct reading")
	}
}
