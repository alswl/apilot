package integrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
)

// WorkbenchCredentialStore stores connection credentials independently of SQLite.
type WorkbenchCredentialStore interface {
	Put(context.Context, string, string) error
	Get(context.Context, string) (string, error)
	Delete(context.Context, string) error
	List(context.Context) ([]string, error)
}

// WorkbenchCredentialUpdater atomically replaces an existing secret.
type WorkbenchCredentialUpdater interface {
	Replace(context.Context, string, string) error
}

// WorkbenchCredentialNamespace isolates credentials belonging to different data directories.
func WorkbenchCredentialNamespace(dataDir string) (string, error) {
	path, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(filepath.Clean(path)))
	return "com.alswl.apilot.github." + hex.EncodeToString(sum[:]), nil
}
