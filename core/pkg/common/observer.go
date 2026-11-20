package common

import (
	"time"
)

type Observer struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Executable    string            `json:"executable"`
	Args          []string          `json:"args"`
	Cwd           string            `json:"cwd"`
	Env           map[string]string `json:"-"`
	EnvKeys       []string          `json:"env_keys"`
	IntervalMS    int64             `json:"interval_ms"`
	TimeoutMS     int64             `json:"timeout_ms"`
	Enabled       bool              `json:"enabled"`
	OutputMode    string            `json:"output_mode"`
	Revision      int64             `json:"revision"`
	LastCheckedAt *time.Time        `json:"last_checked_at"`
	Health        string            `json:"health"`
	Result        string            `json:"result"`
	Error         string            `json:"error"`
	LastExitCode  *int              `json:"last_exit_code"`
	Truncated     bool              `json:"truncated"`
}

type ObserverInput struct {
	EnvUnset   []string          `json:"env_unset"`
	Name       string            `json:"name"`
	Executable string            `json:"executable"`
	Args       []string          `json:"args"`
	Cwd        string            `json:"cwd"`
	EnvSet     map[string]string `json:"env_set"`
	IntervalMS int64             `json:"interval_ms"`
	TimeoutMS  int64             `json:"timeout_ms"`
	Enabled    bool              `json:"enabled"`
	OutputMode string            `json:"output_mode"`
}
