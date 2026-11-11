package model

import "time"

type Schedule struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Expression string     `json:"expression"`
	Enabled    bool       `json:"enabled"`
	Timezone   string     `json:"timezone"`
	NextRunAt  *time.Time `json:"next_run_at"`
}
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
type Run struct {
	ScheduledAt *time.Time `json:"scheduled_at"`
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	TaskName    string     `json:"task_name"`
	Executable  string     `json:"executable"`
	Args        []string   `json:"args"`
	Trigger     string     `json:"trigger"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	FinalizedAt *time.Time `json:"finalized_at"`
	DurationMS  *int64     `json:"duration_ms"`
	ExitCode    *int       `json:"exit_code"`
	ErrorCode   string     `json:"error_code,omitempty"`
	Error       string     `json:"error,omitempty"`
	LogPath     string     `json:"log_path"`
	Truncated   bool       `json:"truncated"`
}

func (r Run) Active() bool { return r.Status == "queued" || r.Status == "running" }

type Settings struct {
	DefaultTimeoutMS int64 `json:"default_timeout_ms"`
	MaxConcurrent    int   `json:"max_concurrent"`
	RetentionDays    int   `json:"retention_days"`
	NotifyFailure    bool  `json:"notify_failure"`
	NotifySuccess    bool  `json:"notify_success"`
	NotifyObserver   bool  `json:"notify_observer"`
	LogLimitBytes    int64 `json:"log_limit_bytes"`
	Revision         int64 `json:"revision"`
}

func DefaultSettings() Settings {
	return Settings{1800000, 4, 30, true, false, true, 50 * 1024 * 1024, 1}
}

type AttentionEvent struct {
	ID             string     `json:"id"`
	SourceType     string     `json:"source_type"`
	SourceID       string     `json:"source_id"`
	RunID          string     `json:"run_id"`
	Kind           string     `json:"kind"`
	Summary        string     `json:"summary"`
	CreatedAt      time.Time  `json:"created_at"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
}
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *Error) Error() string        { return e.Message }
func Err(code, message string) *Error { return &Error{code, message, map[string]any{}} }

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

func CanTransition(from, to string) bool {
	switch from {
	case "queued":
		switch to {
		case "running", "failed", "canceled", "interrupted", "skipped":
			return true
		}
	case "running":
		switch to {
		case "succeeded", "failed", "canceled", "interrupted":
			return true
		}
	}
	return false
}
