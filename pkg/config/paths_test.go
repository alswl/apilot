package config

import (
	"path/filepath"
	"testing"
)

func TestProfilePathsRemainCompatible(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APILOT_DATA_DIR", dir)
	if DataDir() != dir || filepath.Base(SocketPath(dir)) != "ipc.sock" {
		t.Fatal(DataDir(), SocketPath(dir))
	}
}
