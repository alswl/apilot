package managers

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/robfig/cron/v3"
)

func Next(s *common.Schedule, now time.Time) (time.Time, error) {
	if s == nil {
		return time.Time{}, nil
	}
	switch s.Type {
	case "interval":
		d, e := time.ParseDuration(s.Expression)
		if e != nil || d < time.Second {
			return time.Time{}, fmt.Errorf("interval must be at least 1s")
		}
		return now.Add(d), nil
	case "cron":
		loc, e := time.LoadLocation(s.Timezone)
		if e != nil {
			return time.Time{}, fmt.Errorf("invalid timezone")
		}
		rule, e := cron.ParseStandard(s.Expression)
		if e != nil {
			return time.Time{}, fmt.Errorf("invalid cron expression: %w", e)
		}
		n := rule.Next(now.In(loc))
		if n.IsZero() {
			return n, fmt.Errorf("cron has no next occurrence")
		}
		return n.UTC(), nil
	default:
		return time.Time{}, fmt.Errorf("schedule type must be cron or interval")
	}
}

func SystemTimezone() (string, error) {
	if zone := os.Getenv("TZ"); zone != "" && zone != "Local" {
		if _, e := time.LoadLocation(zone); e == nil {
			return zone, nil
		}
	}
	path, e := os.Readlink("/etc/localtime")
	if e == nil {
		if _, zone, ok := strings.Cut(path, "zoneinfo/"); ok {
			if _, e = time.LoadLocation(zone); e == nil {
				return zone, nil
			}
		}
	}
	return "", fmt.Errorf("Cannot determine system timezone; choose an explicit IANA timezone")
}
