package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/alswl/apilot/pkg/common"
)

type workbenchObsidianCLI struct {
	executable string
	vault      string
	basePath   string
}

func openWorkbenchObsidianCLI(ctx context.Context, c common.WorkbenchConnection) (workbenchObsidianCLI, error) {
	var cli workbenchObsidianCLI
	for _, name := range []string{"obsidian", "/usr/local/bin/obsidian", "/opt/homebrew/bin/obsidian"} {
		if path, err := exec.LookPath(name); err == nil {
			cli.executable = path
			break
		}
	}
	if cli.executable == "" {
		return cli, common.Err("CONFLICT", "Obsidian CLI was not found")
	}
	// Validate the scope using metadata only. The CLI supplies the note list and contents.
	root, err := openWorkbenchNoteRoot(c)
	if err != nil {
		return cli, common.Err("CONFLICT", "Obsidian vault or scope is unavailable")
	}
	root.Close()
	cli.vault = filepath.Base(c.Vault)
	path, err := cli.run(ctx, "vault", "info=path")
	if err != nil {
		return cli, err
	}
	cli.basePath = strings.TrimRight(string(path), "\r\n")
	selected, selectedErr := os.Stat(cli.basePath)
	expected, expectedErr := os.Stat(c.Vault)
	if selectedErr != nil || expectedErr != nil || !os.SameFile(selected, expected) {
		return cli, common.Err("CONFLICT", "Obsidian CLI did not select the configured vault")
	}
	return cli, nil
}

func (cli workbenchObsidianCLI) run(ctx context.Context, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cli.executable, append([]string{"vault=" + cli.vault, command}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return nil, common.Err("CONFLICT", "Could not start Obsidian CLI")
	}
	const maxOutput = 8 << 20
	stop := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	defer stop()
	output, err := io.ReadAll(io.LimitReader(stdout, maxOutput+1))
	if err != nil || len(output) > maxOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, common.Err("CONFLICT", "Obsidian CLI output could not be read within the size limit")
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return nil, common.Err("CONFLICT", "Obsidian CLI did not finish before the read deadline")
	}
	switch {
	case bytes.HasPrefix(output, []byte("Command line interface is not enabled.")):
		return nil, common.Err("CONFLICT", "Enable the command line interface in Obsidian settings")
	case bytes.Equal(bytes.TrimSpace(output), []byte("Vault not found.")):
		return nil, common.Err("CONFLICT", "Obsidian CLI could not find the configured vault")
	case waitErr != nil, bytes.HasPrefix(output, []byte("The CLI is unable to find Obsidian.")), bytes.HasPrefix(output, []byte("Error: ")):
		return nil, common.Err("CONFLICT", "Obsidian CLI could not read the vault. Check that Obsidian is running with CLI enabled.")
	}
	return output, nil
}

func (cli workbenchObsidianCLI) notes(ctx context.Context, scope string) ([]common.WorkbenchNote, error) {
	output, err := cli.run(ctx, "files")
	if err != nil {
		return nil, err
	}
	notes := []common.WorkbenchNote{}
	listing := strings.TrimRight(string(output), "\r\n")
	if listing == "" || listing == "No files found." || listing == "No files found" {
		return notes, nil
	}
	for path := range strings.SplitSeq(listing, "\n") {
		path = strings.TrimSuffix(path, "\r")
		if !filepath.IsLocal(path) || filepath.Clean(path) != path {
			return nil, common.Err("CONFLICT", "Obsidian CLI returned an invalid note list")
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") || !workbenchVisiblePath(path) {
			continue
		}
		if filepath.Clean(scope) == "." || strings.HasPrefix(path, filepath.ToSlash(scope)+"/") {
			notes = append(notes, common.WorkbenchNote{NotePath: path})
		}
	}
	slices.SortFunc(notes, func(a, b common.WorkbenchNote) int { return strings.Compare(a.NotePath, b.NotePath) })
	return slices.Compact(notes), nil
}

func readWorkbenchObsidianCLI(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
	cli, err := openWorkbenchObsidianCLI(ctx, c)
	if err != nil {
		return nil, err
	}
	notes, err := cli.notes(ctx, c.Scope)
	if err != nil {
		return nil, err
	}
	tasks := []common.WorkbenchTask{}
	reportWorkbenchReadProgress(ctx, 0, len(notes))
	for read := 0; read < len(notes); {
		batch := notes[read:min(read+32, len(notes))]
		contents, err := cli.readBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		for i, content := range contents {
			for _, task := range parseWorkbenchNote(c, batch[i].NotePath, content) {
				if len(task.Summary) > 12<<10 {
					return nil, common.Err("CONFLICT", "Obsidian task line exceeds the read limit")
				}
				tasks = append(tasks, task)
			}
		}
		read += len(contents)
		reportWorkbenchReadProgress(ctx, read, len(notes))
	}
	return normalizeWorkbenchObsidianIDs(tasks), nil
}

// Use the same cached Markdown read as the CLI read command, with a bounded
// response so vaults containing large notes can still be read in batches.
func (cli workbenchObsidianCLI) readBatch(ctx context.Context, notes []common.WorkbenchNote) ([]string, error) {
	paths := make([]string, len(notes))
	for i, note := range notes {
		paths[i] = note.NotePath
	}
	pathJSON, _ := json.Marshal(paths)
	vaultJSON, _ := json.Marshal(cli.basePath)
	script := `(async () => {
        const vault = app.vault;
        if (vault.adapter.getBasePath() !== ` + string(vaultJSON) + `) {
            throw new Error("Unexpected vault");
        }
        const files = ` + string(pathJSON) + `;
        const encoder = new TextEncoder();
        const output = [];
        let size = 0;
        for (const path of files) {
            const file = vault.getFileByPath(path);
            if (!file) throw new Error("Note no longer exists");
            if (file.stat.size > 8388608) throw new Error("Note exceeds read limit");
            const entry = JSON.stringify(await vault.cachedRead(file));
            size += encoder.encode(entry).length + 1;
            if (size > 8388500) {
                if (!output.length) throw new Error("Note exceeds CLI response limit");
                break;
            }
            output.push(entry);
        }
        return "[" + output.join(",") + "]";
    })()`
	output, err := cli.run(ctx, "eval", "code="+script)
	if err != nil {
		return nil, err
	}
	result, ok := bytes.CutPrefix(output, []byte("=> "))
	var contents []string
	if !ok || json.Unmarshal(result, &contents) != nil || len(contents) == 0 || len(contents) > len(notes) {
		return nil, common.Err("CONFLICT", "Obsidian CLI returned an invalid batch of notes")
	}
	return contents, nil
}

func ReadWorkbenchObsidianTarget(ctx context.Context, c common.WorkbenchConnection, notePath string) ([]byte, error) {
	if c.ReadMethod != common.WorkbenchObsidianCLI {
		return ReadWorkbenchObsidianNote(c, notePath)
	}
	if !workbenchVisiblePath(notePath) || !strings.EqualFold(filepath.Ext(notePath), ".md") ||
		(filepath.Clean(c.Scope) != "." && !strings.HasPrefix(notePath, filepath.ToSlash(c.Scope)+"/")) {
		return nil, common.Err("INVALID_ARGUMENT", "Choose a Markdown note within the connection scope")
	}
	cli, err := openWorkbenchObsidianCLI(ctx, c)
	if err != nil {
		return nil, err
	}
	return cli.run(ctx, "read", "path="+notePath)
}
