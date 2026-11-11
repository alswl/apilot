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

func brandTheme(c *ui.Context) {
	t := *c.Theme()
	if t.Dark {
		t.Background = ui.Hex("#151b30")
		t.Surface = ui.Hex("#242d48")
		t.SurfaceHover = ui.Hex("#323d60")
		t.Accent = ui.Hex("#a795ff")
		t.AccentHover = ui.Hex("#baacff")
		t.AccentText = ui.Hex("#161027")
		t.Success = ui.Hex("#59ddca")
	} else {
		t.Background = ui.Hex("#f5f7ff")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#e9edff")
		t.Accent = ui.Hex("#6243dc")
		t.AccentHover = ui.Hex("#7657ea")
		t.AccentText = ui.Hex("#ffffff")
		t.Success = ui.Hex("#007d72")
	}
	t.AccentPressed = ui.Hex("#5031be")
	t.Focus = t.Accent
	t.Radius = 8
	c.SetTheme(&t)
}

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
