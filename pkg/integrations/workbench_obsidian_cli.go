package integrations

import (
	"bytes"
	"context"
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
	selected, selectedErr := os.Stat(strings.TrimRight(string(path), "\r\n"))
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
	output, err := io.ReadAll(io.LimitReader(stdout, maxOutput+1))
	if err != nil || len(output) > maxOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
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
	for _, note := range notes {
		content, err := cli.run(ctx, "read", "path="+note.NotePath)
		if err != nil {
			return nil, err
		}
		for _, task := range parseWorkbenchNote(c, note.NotePath, string(content)) {
			if len(task.Summary) > 12<<10 {
				return nil, common.Err("CONFLICT", "Obsidian task line exceeds the read limit")
			}
			tasks = append(tasks, task)
		}
	}
	return normalizeWorkbenchObsidianIDs(tasks), nil
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
