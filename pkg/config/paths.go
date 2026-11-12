package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func DataDir() string {
	if d := os.Getenv("APILOT_DATA_DIR"); d != "" {
		p, _ := filepath.Abs(d)
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "apilot")
}

func SocketPath(dir string) string {
	cache, _ := os.UserCacheDir()
	hash := sha256.Sum256([]byte(dir))
	return filepath.Join(cache, "apilot", hex.EncodeToString(hash[:5]), "ipc.sock")
}
