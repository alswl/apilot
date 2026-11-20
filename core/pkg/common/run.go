package common

import (
	"time"
)

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
