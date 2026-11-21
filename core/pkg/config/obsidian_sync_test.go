package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadObsidianSync(t *testing.T) {
	dir := t.TempDir()
	defaults, err := LoadObsidianSync(dir)
	if err != nil || defaults.ScanInterval != time.Minute || defaults.ScanTimeout != 30*time.Second {
		t.Fatalf("defaults: %+v, %v", defaults, err)
	}
	path := filepath.Join(dir, "obsidian-sync.json")
	if err := os.WriteFile(path, []byte(`{"scan_interval_seconds":5,"max_retries":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := LoadObsidianSync(dir)
	if err != nil || settings.ScanInterval != 5*time.Second || settings.ScanTimeout != defaults.ScanTimeout || settings.MaxRetries != 0 {
		t.Fatalf("settings: %+v, %v", settings, err)
	}
	for _, input := range []string{`null`, `{"scan_interval_seconds":null}`, `{"scan_timeout_seconds":null}`, `{"max_retries":null}`, `{"scan_interval_seconds":5,"scan_timeout_seconds":0}`, `{"unknown":1}`, `{} {}`} {
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := LoadObsidianSync(dir)
		if err == nil || !strings.Contains(err.Error(), path) || got != defaults {
			t.Errorf("input %s: got %+v, %v", input, got, err)
		}
	}
}
