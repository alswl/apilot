package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchObsidianCLI struct {
	executable string
	vault      string
	basePath   string
}

var errObsidianCLICommandTimeout = errors.New("Obsidian CLI command timed out")
var errObsidianCLIIncompleteBatch = errors.New("Obsidian CLI returned an incomplete batch")

type obsidianCLIDiagnostic []byte

func (d *obsidianCLIDiagnostic) Write(p []byte) (int, error) {
	const limit = 1024
	*d = append(*d, p[:min(len(p), limit-len(*d))]...)
	return len(p), nil
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
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	interrupted := func() error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if commandCtx.Err() != nil {
			return errors.Join(common.Err("CONFLICT", "Obsidian CLI command "+command+" timed out after 15s"), errObsidianCLICommandTimeout)
		}
		return nil
	}
	cmd := exec.CommandContext(commandCtx, cli.executable, append([]string{"vault=" + cli.vault, command}, args...)...)
	var stderr obsidianCLIDiagnostic
	cmd.Stderr = &stderr
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		if stopErr := interrupted(); stopErr != nil {
			return nil, stopErr
		}
		return nil, common.Err("CONFLICT", "Could not start Obsidian CLI")
	}
	const maxOutput = 8 << 20
	stop := context.AfterFunc(commandCtx, func() { _ = stdout.Close() })
	defer stop()
	output, err := io.ReadAll(io.LimitReader(stdout, maxOutput+1))
	if err != nil || len(output) > maxOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if stopErr := interrupted(); stopErr != nil {
			return nil, stopErr
		}
		return nil, common.Err("CONFLICT", "Obsidian CLI output could not be read within the size limit")
	}
	waitErr := cmd.Wait()
	if stopErr := interrupted(); stopErr != nil {
		return nil, stopErr
	}
	hasDiagnostic := func(prefix string) bool {
		return bytes.HasPrefix(output, []byte(prefix)) || bytes.HasPrefix(stderr, []byte(prefix))
	}
	switch {
	case hasDiagnostic("The CLI is unable to find Obsidian."):
		return nil, common.Err("IPC_UNAVAILABLE", "Obsidian CLI could not connect to Obsidian. Reopen Obsidian and try again.")
	case hasDiagnostic("Command line interface is not enabled."):
		return nil, common.Err("CONFLICT", "Enable the command line interface in Obsidian settings")
	case bytes.Equal(bytes.TrimSpace(output), []byte("Vault not found.")):
		return nil, common.Err("CONFLICT", "Obsidian CLI could not find the configured vault")
	case waitErr != nil, hasDiagnostic("Error: "):
		return nil, common.Err("CONFLICT", "Obsidian CLI command "+command+" failed")
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
		size := min(32, len(notes)-read)
		batch := notes[read : read+size]
		contents, err := cli.readBatch(ctx, batch)
		// A timed-out read or incomplete reply has no side effects. Split the
		// current batch, keeping already-confirmed batches in memory.
		for err != nil && len(batch) > 1 && ctx.Err() == nil &&
			(errors.Is(err, errObsidianCLICommandTimeout) || errors.Is(err, errObsidianCLIIncompleteBatch)) {
			batch = batch[:(len(batch)+1)/2]
			contents, err = cli.readBatch(ctx, batch)
		}
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
        const marker = "apilot-note-batch-v1:";
        const vault = app.vault;
        if (vault.adapter.getBasePath() !== ` + string(vaultJSON) + `) {
            return marker + "wrong-vault";
        }
        const files = ` + string(pathJSON) + `;
        const encoder = new TextEncoder();
        const output = [];
        let size = 0;
        for (const path of files) {
            const file = vault.getFileByPath(path);
            if (!file) return marker + "missing-note";
            if (file.stat.size > 8388608) return marker + "note-too-large";
            const entry = JSON.stringify(await vault.cachedRead(file));
            size += encoder.encode(entry).length + 1;
            if (size > 8388500) {
                if (!output.length) return marker + "note-too-large";
                break;
            }
            output.push(entry);
        }
        return marker + "[" + output.join(",") + "]";
    })()`
	output, err := cli.run(ctx, "eval", "code="+script)
	if err != nil {
		return nil, err
	}
	const marker = "=> apilot-note-batch-v1:"
	// Obsidian eval includes console messages before the result.
	for line := range bytes.SplitSeq(output, []byte("\n")) {
		result, ok := bytes.CutPrefix(bytes.TrimSuffix(line, []byte("\r")), []byte(marker))
		if !ok {
			continue
		}
		switch string(result) {
		case "wrong-vault":
			return nil, common.Err("CONFLICT", "Obsidian CLI selected a different vault during the scan")
		case "missing-note":
			return nil, common.Err("CONFLICT", "An Obsidian note disappeared during the scan. Synchronize again.")
		case "note-too-large":
			return nil, common.Err("CONFLICT", "An Obsidian note exceeded the CLI batch size limit")
		}
		var contents []string
		if json.Unmarshal(result, &contents) == nil && len(contents) > 0 && len(contents) <= len(notes) {
			return contents, nil
		}
		break
	}
	return nil, errors.Join(common.Err("CONFLICT", "Obsidian CLI did not return a complete Markdown batch"), errObsidianCLIIncompleteBatch)
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
	contents, err := cli.readBatch(ctx, []common.WorkbenchNote{{NotePath: notePath}})
	if err != nil {
		return nil, err
	}
	return []byte(contents[0]), nil
}
