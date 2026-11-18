package ui

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/tray.png
var menuBarIcon []byte

//go:embed assets/app.png
var appIcon []byte

var appMark, _ = ui.DecodeBitmap(appIcon)

func resultColor(c *ui.Context, status string) ui.Color {
	switch status {
	case "succeeded":
		return c.Theme().Success
	case "failed", "interrupted":
		return c.Theme().Danger
	case "running", "queued":
		return c.Theme().Accent
	default:
		return c.Theme().TextMuted
	}
}
