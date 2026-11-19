package integrations

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alswl/apilot/pkg/common"
)

var errVaultChanged = errors.New("vault changed during scan")

func ReadWorkbenchObsidian(ctx context.Context, c common.WorkbenchConnection) ([]common.WorkbenchTask, error) {
	vault, err := os.OpenRoot(c.Vault)
	if err != nil {
		return nil, err
	}
	defer vault.Close()
	scope := filepath.Clean(c.Scope)
	for part := scope; part != "."; part = filepath.Dir(part) {
		if !filepath.IsLocal(part) || strings.HasPrefix(filepath.Base(part), ".") {
			return nil, errVaultChanged
		}
		info, err := vault.Lstat(part)
		if err != nil || !info.IsDir() {
			return nil, errVaultChanged
		}
	}
	expected, err := vault.Lstat(scope)
	if err != nil {
		return nil, err
	}
	root, err := vault.OpenRoot(scope)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(expected, opened) {
		return nil, errVaultChanged
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
				return err
			}
			return handler(path, info)
		})
	}
	err = walk(func(path string, info fs.FileInfo) error {
		f, err := root.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		actual, err := f.Stat()
		if err != nil || !sameWorkbenchFile(info, actual) {
			return errVaultChanged
		}
		const maxNote = 8 << 20
		content, err := io.ReadAll(io.LimitReader(f, maxNote+1))
		if err != nil {
			return err
		}
		if len(content) > maxNote {
			return errors.New("note exceeds read limit")
		}
		actual, err = f.Stat()
		if err != nil || !sameWorkbenchFile(info, actual) {
			return errVaultChanged
		}
		files[path] = actual
		parsed := parseWorkbenchNote(c, filepath.ToSlash(filepath.Join(scope, path)), string(content))
		for _, task := range parsed {
			// Bound a page of task payloads below the existing IPC frame limit.
			if len(task.Summary) > 12<<10 {
				return errors.New("task line exceeds read limit")
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
			return errVaultChanged
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
		return nil, errVaultChanged
	}
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
	return all, nil
}

func sameWorkbenchFile(a, b fs.FileInfo) bool {
	return a != nil && b != nil && a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
