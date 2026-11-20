package integrations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
)

var errVaultChanged = errors.New("vault changed during scan")

func ReadWorkbenchObsidian(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
	if c.ReadMethod == common.WorkbenchObsidianCLI {
		return readWorkbenchObsidianCLI(ctx, c)
	}
	vault, err := os.OpenRoot(c.Vault)
	if err != nil {
		return nil, obsidianScanError("Open Obsidian vault", "", err)
	}
	defer vault.Close()
	scope := filepath.Clean(c.Scope)
	for part := scope; part != "."; part = filepath.Dir(part) {
		if !filepath.IsLocal(part) || strings.HasPrefix(filepath.Base(part), ".") {
			return nil, obsidianScanError("Validate sync scope", "", fs.ErrInvalid)
		}
		info, err := vault.Lstat(part)
		if err != nil {
			return nil, obsidianScanError("Access sync scope", part, err)
		}
		if !info.IsDir() {
			return nil, obsidianScanError("Access sync scope", part, fs.ErrInvalid)
		}
	}
	expected, err := vault.Lstat(scope)
	if err != nil {
		return nil, obsidianScanError("Access sync scope", scope, err)
	}
	root, err := vault.OpenRoot(scope)
	if err != nil {
		return nil, obsidianScanError("Open sync scope", scope, err)
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, opened) {
		return nil, obsidianScanError("Verify vault contents", scope, errVaultChanged)
	}
	all := []common.WorkbenchTask{}
	files := map[string]fs.FileInfo{}
	walk := func(handler func(string, fs.FileInfo) error) error {
		return fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				return err
			}
			if path != "." && strings.HasPrefix(entry.Name(), ".") {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".md") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return obsidianScanError("Inspect Markdown note", filepath.Join(scope, path), err)
			}
			return handler(path, info)
		})
	}
	err = walk(func(path string, info fs.FileInfo) error {
		notePath := filepath.ToSlash(filepath.Join(scope, path))
		f, err := root.Open(path)
		if err != nil {
			return obsidianScanError("Read Markdown note", notePath, err)
		}
		defer f.Close()
		actual, err := f.Stat()
		if err != nil || !sameWorkbenchFile(info, actual) {
			return obsidianScanError("Verify Markdown note", filepath.Join(scope, path), errVaultChanged)
		}
		const maxNote = 8 << 20
		content, err := io.ReadAll(io.LimitReader(f, maxNote+1))
		if err != nil {
			return obsidianScanError("Read Markdown note", notePath, err)
		}
		if len(content) > maxNote {
			return common.Err("CONFLICT", fmt.Sprintf("Read Markdown note %q: note exceeds 8 MiB", notePath))
		}
		actual, err = f.Stat()
		if err != nil || !sameWorkbenchFile(info, actual) {
			return obsidianScanError("Verify Markdown note", filepath.Join(scope, path), errVaultChanged)
		}
		files[path] = actual
		parsed := parseWorkbenchNote(c, notePath, string(content))
		for _, task := range parsed {
			// Bound a page of task payloads below the existing IPC frame limit.
			if len(task.Summary) > 12<<10 {
				return common.Err("CONFLICT", fmt.Sprintf("Parse Markdown note %q: task line exceeds 12 KiB", notePath))
			}
		}
		all = append(all, parsed...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = walk(func(path string, info fs.FileInfo) error {
		if !sameWorkbenchFile(files[path], info) {
			return obsidianScanError("Verify Markdown note", filepath.Join(scope, path), errVaultChanged)
		}
		delete(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	current, err := vault.Lstat(scope)
	currentVault, vaultErr := os.Stat(c.Vault)
	originalVault, originalErr := vault.Stat(".")
	if err != nil || vaultErr != nil || originalErr != nil || len(files) != 0 || !os.SameFile(current, expected) || !os.SameFile(currentVault, originalVault) {
		return nil, obsidianScanError("Verify vault contents", scope, errVaultChanged)
	}
	return normalizeWorkbenchObsidianIDs(all), nil
}

func normalizeWorkbenchObsidianIDs(all []common.WorkbenchTask) []common.WorkbenchTask {
	ids := map[string]int{}
	for _, task := range all {
		if task.NativeID != "" {
			ids[task.NativeID]++
		}
	}
	for i := range all {
		task := &all[i]
		if ids[task.NativeID] > 1 {
			task.SourceKey = workbenchNoteKey(task.NativeFields["path"], task.Title)
		}
	}
	return all
}

func sameWorkbenchFile(a, b fs.FileInfo) bool {
	return a != nil && b != nil && a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func obsidianScanError(action, path string, err error) error {
	reason := "filesystem read failed"
	switch {
	case errors.Is(err, fs.ErrPermission):
		reason = "permission denied"
	case errors.Is(err, fs.ErrNotExist):
		reason = "path does not exist"
	case errors.Is(err, fs.ErrInvalid):
		reason = "expected a valid directory"
	case errors.Is(err, errVaultChanged):
		reason = "vault files changed during the scan"
	}
	if path != "" && path != "." {
		action += fmt.Sprintf(" %q", filepath.ToSlash(path))
	}
	return common.Err("CONFLICT", action+": "+reason)
}
