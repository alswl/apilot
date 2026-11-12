package common

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
