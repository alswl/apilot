package config

import (
	"encoding/json"
	"fmt"
	"os"
)

const DefaultKeyboardMode = "vim"

type Keybindings struct {
	Mode             string              `json:"mode,omitempty"`
	Version          int                 `json:"version"`
	EnableSingleKeys bool                `json:"enableSingleKeys"`
	Bindings         map[string][]string `json:"bindings"`
}

// ReadKeybindings reads overrides. An empty path selects the built-in profile.
func ReadKeybindings(path string) (Keybindings, error) {
	settings := Keybindings{Version: 1}
	if path == "" {
		settings.Mode = DefaultKeyboardMode
		return settings, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return settings, err
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return settings, fmt.Errorf("read keybindings: %w", err)
	}
	if settings.Version != 1 {
		return settings, fmt.Errorf("unsupported keybindings version %d", settings.Version)
	}
	return settings, nil
}
