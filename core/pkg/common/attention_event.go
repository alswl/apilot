package common

import (
	"time"
)

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
