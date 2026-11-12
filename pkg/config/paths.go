package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
)

func DataDir() string {
	if d := os.Getenv("APILOT_DATA_DIR"); d != "" {
		p, _ := filepath.Abs(d)
		return p
	}
	return defaultDataDir(runtime.GOOS)
}

func defaultDataDir(goos string) string {
	home, _ := os.UserHomeDir()
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "apilot")
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, "apilot")
	default:
		base := os.Getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(base) {
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "apilot")
	}
}

func SocketPath(dir string) string {
	cache, _ := os.UserCacheDir()
	hash := sha256.Sum256([]byte(dir))
	return filepath.Join(cache, "apilot", hex.EncodeToString(hash[:5]), "ipc.sock")
}
