package ui

import (
	"sync/atomic"

	"github.com/alswl/apilot/internal/ui/locales"
	"github.com/egoist/mygo/ui"
)

// Cache the selected language after MyGo starts: offscreen UI tests have no
// native application loop to answer App.Locale on the main thread.
var displayLocale atomic.Value

func translate(locale, key string) string { return locales.Catalog.Format(locale, key, nil) }

func tr(key string) string {
	locale, _ := displayLocale.Load().(string)
	return translate(locale, key)
}

func languagePicker(c *ui.Context, selected *string, apply func(string)) {
	ui.Field(c, tr("language"), func() {
		ui.RadioGroup(c, func() {
			ui.Row(c).Gap(12).Children(func() {
				for _, option := range []struct{ tag, name string }{{"", tr("systemLanguage")}, {"en", "English"}, {"zh-Hans", "简体中文"}} {
					if ui.Radio(c, selected, option.tag, option.name).Changed() {
						apply(*selected)
					}
				}
			})
		}).Label(tr("language"))
	})
}
