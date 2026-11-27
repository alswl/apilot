package workbench

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTaskQueryConfigWriteInterruption(t *testing.T) {
	const fault = "APILOT_TEST_TASK_VIEW_INTERRUPT"
	if mode := os.Getenv(fault); mode != "" {
		path := os.Getenv("APILOT_TEST_TASK_VIEW_PATH")
		switch mode {
		case "migration":
			_, err := bootTaskQueryConfig(path)
			t.Fatalf("migration returned before reaching the exit hook: %v", err)
		case "save":
			m := newFramework()
			cfg, err := readTaskQueryConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			m.customQuery.path = path
			m.adoptTaskQueryConfig(cfg)
			err = m.updateTaskQueryConfig(func(cfg *taskQueryConfig) error {
				cfg.Views[0].Name = "Updated"
				return nil
			})
			t.Fatalf("save returned before reaching the exit hook: %v", err)
		default:
			t.Fatalf("unknown interruption mode %q", mode)
		}
	}
	if os.Getenv("APILOT_TEST_TASK_VIEW_OVERLAY") != "1" {
		t.Skip("requires scripts/test-task-view-write-interruption.py")
	}

	for _, mode := range []string{"migration", "save"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			legacy := filepath.Join(dir, "task-views.json")
			path := filepath.Join(dir, "task-views.v2.json")
			legacyBytes := []byte("[\n  {\"id\":\"saved\",\"name\":\"Original\",\"state\":{}}\n]\n")
			mustRead := func(file string) []byte {
				t.Helper()
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				return data
			}
			if err := os.WriteFile(legacy, legacyBytes, 0600); err != nil {
				t.Fatal(err)
			}
			var prior []byte
			if mode == "save" {
				if _, err := bootTaskQueryConfig(path); err != nil {
					t.Fatal(err)
				}
				prior = mustRead(path)
			}
			child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTaskQueryConfigWriteInterruption$")
			child.Env = append(os.Environ(), fault+"="+mode, "APILOT_TEST_TASK_VIEW_PATH="+path)
			out, err := child.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 93 {
				t.Fatalf("expected pre-rename exit 93, got %v\n%s", err, out)
			}
			if !bytes.Equal(mustRead(legacy), legacyBytes) {
				t.Fatal("interruption changed legacy bytes")
			}
			if mode == "migration" {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("interrupted migration published v2: %v", err)
				}
			} else if !bytes.Equal(mustRead(path), prior) {
				t.Fatal("interrupted save changed the published v2")
			}
			files, err := filepath.Glob(filepath.Join(dir, ".task-views-*.json"))
			if err != nil || len(files) != 1 {
				t.Fatalf("expected one staged file after abrupt exit: %v, %v", files, err)
			}
			staged, err := readTaskQueryConfig(files[0])
			wantName, wantRevision := "Original", int64(1)
			if mode == "save" {
				wantName, wantRevision = "Updated", 2
			}
			if err != nil || staged.Revision != wantRevision || len(staged.Views) != 1 ||
				staged.Views[0].ID != "saved" || staged.Views[0].Name != wantName {
				t.Fatalf("staged configuration: %+v, %v", staged, err)
			}

			reopened, err := bootTaskQueryConfig(path)
			if err != nil || reopened.Revision != 1 || len(reopened.Views) != 1 ||
				reopened.Views[0].ID != "saved" || reopened.Views[0].Name != "Original" {
				t.Fatalf("restart did not recover the original view: %+v, %v", reopened, err)
			}
			if !bytes.Equal(mustRead(legacy), legacyBytes) || (prior != nil && !bytes.Equal(mustRead(path), prior)) {
				t.Fatal("restart changed previously published bytes")
			}
		})
	}
}
