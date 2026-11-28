package platform

import (
	"os"
	"strings"
	"time"
)

// Timezone returns the local IANA timezone, using UTC when it cannot be identified.
func Timezone() string {
	if zone, err := os.Readlink("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(zone, "zoneinfo/"); ok && name != "" {
			if _, err := time.LoadLocation(name); err == nil {
				return name
			}
		}
	}
	name := time.Local.String()
	if _, err := time.LoadLocation(name); err == nil && name != "Local" {
		return name
	}
	return "UTC"
}
