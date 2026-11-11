package ui

import (
	"sync/atomic"

	"github.com/alswl/apilot/internal/ui/locales"
)

// Cache the selected language after MyGo starts: offscreen UI tests have no
// native application loop to answer App.Locale on the main thread.
var displayLocale atomic.Value

func translate(locale, key string) string { return locales.Catalog.Format(locale, key, nil) }

func tr(key string) string {
	locale, _ := displayLocale.Load().(string)
	return translate(locale, key)
}
