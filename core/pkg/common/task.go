package common

import (
	"time"
)

type Task struct {
	NextRunAt   *time.Time        `json:"next_run_at"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Executable  string            `json:"executable"`
	Args        []string          `json:"args"`
	Cwd         string            `json:"cwd"`
	Env         map[string]string `json:"-"`
	EnvKeys     []string          `json:"env_keys"`
	Enabled     bool              `json:"enabled"`
	TimeoutMS   int64             `json:"timeout_ms"`
	Revision    int64             `json:"revision"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Schedule    *Schedule         `json:"schedule"`
	LastRun     *Run              `json:"last_run"`
}

type TaskInput struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Executable  string            `json:"executable"`
	Args        []string          `json:"args"`
	Cwd         string            `json:"cwd"`
	EnvSet      map[string]string `json:"env_set"`
	EnvUnset    []string          `json:"env_unset"`
	Enabled     bool              `json:"enabled"`
	TimeoutMS   int64             `json:"timeout_ms"`
	Schedule    *Schedule         `json:"schedule"`
}
