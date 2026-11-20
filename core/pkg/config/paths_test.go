package config

import (
	"os"
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

func TestDefaultDataDirectories(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", base)
	t.Setenv("LOCALAPPDATA", base)
	for _, tc := range []struct{ platform, want string }{
		{"darwin", filepath.Join(home, "Library", "Application Support", "apilot")},
		{"linux", filepath.Join(base, "apilot")},
		{"windows", filepath.Join(base, "apilot")},
	} {
		if got := defaultDataDir(tc.platform); got != tc.want {
			t.Errorf("%s: %s != %s", tc.platform, got, tc.want)
		}
	}
	t.Setenv("XDG_DATA_HOME", "relative")
	if got := defaultDataDir("linux"); got != filepath.Join(home, ".local", "share", "apilot") {
		t.Fatal(got)
	}
	t.Setenv("LOCALAPPDATA", "")
	if got := defaultDataDir("windows"); got != filepath.Join(home, "AppData", "Local", "apilot") {
		t.Fatal(got)
	}
}
