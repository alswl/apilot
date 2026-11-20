package integrations

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/fsnotify/fsnotify"
)

// WatchWorkbenchObsidian reports changes as hints; callers must reread the source.
// Returning an error means the caller must arrange a full verification and retry.
func WatchWorkbenchObsidian(ctx context.Context, c common.WorkbenchConnection, changed func(string)) error {
	root, err := openWorkbenchNoteRoot(c)
	if err != nil {
		return err
	}
	defer root.Close()
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()
	vault, err := filepath.EvalSymlinks(c.Vault)
	if err != nil {
		return err
	}
	directory := filepath.Join(vault, c.Scope)
	registered := map[string]bool{}
	addDirs := func() error {
		rootInfo, err := root.Stat(".")
		if err != nil {
			return err
		}
		current, err := os.Stat(directory)
		if err != nil || !os.SameFile(rootInfo, current) {
			return errVaultChanged
		}
		err = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path != directory && strings.HasPrefix(entry.Name(), ".") {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				relative, err := filepath.Rel(directory, path)
				if err != nil {
					return err
				}
				expected, err := root.Lstat(relative)
				if err != nil || !expected.IsDir() {
					return errVaultChanged
				}
				if err := watcher.Add(path); err != nil {
					return err
				}
				actual, err := os.Stat(path)
				if err != nil || !os.SameFile(expected, actual) {
					return errVaultChanged
				}
				registered[path] = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		current, err = os.Stat(directory)
		if err != nil || !os.SameFile(rootInfo, current) {
			return errVaultChanged
		}
		return nil
	}
	if err := addDirs(); err != nil {
		return err
	}
	// Cover changes made while directory watches were being installed.
	changed("")
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-watcher.Errors:
			if !ok {
				return errVaultChanged
			}
			return errVaultChanged
		case event, ok := <-watcher.Events:
			if !ok {
				return errVaultChanged
			}
			if registered[event.Name] && event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
				return errVaultChanged
			}
			relative, err := filepath.Rel(vault, event.Name)
			if err != nil || !workbenchVisiblePath(relative) {
				continue
			}
			if event.Op&fsnotify.Create != 0 {
				info, statErr := os.Lstat(event.Name)
				if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
					return statErr
				}
				if statErr == nil && info.IsDir() {
					// Rewalk to install watches on a new directory and its descendants.
					if err := addDirs(); err != nil {
						return err
					}
				}
			}
			if !registered[event.Name] && !strings.EqualFold(filepath.Ext(event.Name), ".md") {
				continue
			}
			changed(filepath.ToSlash(relative))
		}
	}
}
