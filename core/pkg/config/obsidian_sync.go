package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ObsidianSync controls background verification of Obsidian connections.
type ObsidianSync struct {
	ScanInterval          time.Duration
	ScanTimeout           time.Duration
	MaxRetries            int
	ScanTimeoutConfigured bool
}

// LoadObsidianSync reads optional advanced settings from the data directory.
func LoadObsidianSync(dir string) (ObsidianSync, error) {
	defaults := ObsidianSync{ScanInterval: time.Minute, ScanTimeout: 30 * time.Second, MaxRetries: 3}
	path := filepath.Join(dir, "obsidian-sync.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return defaults, nil
	}
	if err != nil {
		return defaults, fmt.Errorf("read %s: %w", path, err)
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return defaults, fmt.Errorf("parse %s: expected a JSON object", path)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err == nil {
		for _, name := range []string{"scan_interval_seconds", "scan_timeout_seconds", "max_retries"} {
			if raw, ok := fields[name]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return defaults, fmt.Errorf("%s: %s cannot be null", path, name)
			}
		}
	}
	result := defaults
	var input struct {
		ScanIntervalSeconds *int `json:"scan_interval_seconds"`
		ScanTimeoutSeconds  *int `json:"scan_timeout_seconds"`
		MaxRetries          *int `json:"max_retries"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return defaults, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return defaults, fmt.Errorf("parse %s: expected a single JSON object", path)
	}
	if input.ScanIntervalSeconds != nil {
		if *input.ScanIntervalSeconds < 1 || *input.ScanIntervalSeconds > 86400 {
			return defaults, fmt.Errorf("%s: scan_interval_seconds must be between 1 and 86400", path)
		}
		result.ScanInterval = time.Duration(*input.ScanIntervalSeconds) * time.Second
	}
	if input.ScanTimeoutSeconds != nil {
		if *input.ScanTimeoutSeconds < 1 || *input.ScanTimeoutSeconds > 3600 {
			return defaults, fmt.Errorf("%s: scan_timeout_seconds must be between 1 and 3600", path)
		}
		result.ScanTimeout = time.Duration(*input.ScanTimeoutSeconds) * time.Second
		result.ScanTimeoutConfigured = true
	}
	if input.MaxRetries != nil {
		if *input.MaxRetries < 0 || *input.MaxRetries > 100 {
			return defaults, fmt.Errorf("%s: max_retries must be between 0 and 100", path)
		}
		result.MaxRetries = *input.MaxRetries
	}
	return result, nil
}
