package common

import (
	"time"
)

type Schedule struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	Expression string     `json:"expression"`
	Enabled    bool       `json:"enabled"`
	Timezone   string     `json:"timezone"`
	NextRunAt  *time.Time `json:"next_run_at"`
}
