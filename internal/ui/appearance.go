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
		t.Background = ui.Hex("#201a23")
		t.Surface = ui.Hex("#2c2430")
		t.SurfaceHover = ui.Hex("#3c303e")
		t.SurfacePressed = ui.Hex("#4c3948")
		t.Border = ui.Hex("#66515d")
		t.Text = ui.Hex("#fcf4ef")
		t.TextMuted = ui.Hex("#c8b6b8")
		t.Accent = ui.Hex("#ffa78e")
		t.AccentHover = ui.Hex("#ffb9a5")
		t.AccentPressed = ui.Hex("#f18f77")
		t.AccentText = ui.Hex("#2a1821")
		t.Success = ui.Hex("#6ddbcf")
		t.Warning = ui.Hex("#e9b75f")
		t.Danger = ui.Hex("#ff7f99")
		t.Selection = ui.Hex("#654334")
	} else {
		t.Background = ui.Hex("#fcf9f7")
		t.Surface = ui.Hex("#ffffff")
		t.SurfaceHover = ui.Hex("#f8ede9")
		t.SurfacePressed = ui.Hex("#f2ded8")
		t.Border = ui.Hex("#d2c2c7")
		t.Text = ui.Hex("#30222e")
		t.TextMuted = ui.Hex("#74636f")
		t.Accent = ui.Hex("#b5472e")
		t.AccentHover = ui.Hex("#9e3822")
		t.AccentPressed = ui.Hex("#8c2f1f")
		t.AccentText = ui.Hex("#ffffff")
		t.Success = ui.Hex("#087b72")
		t.Warning = ui.Hex("#9c6500")
		t.Danger = ui.Hex("#bb2441")
		t.Selection = ui.Hex("#f7d9cf")
	}
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
