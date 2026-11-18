package components

import (
	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

func Symbol(c *ui.Context, name string, color ui.Color) {
	sfsymbols.Symbol(c, name).Size(16).Natural().Color(color)
}

// IconButton uses the same name for accessibility and the tooltip.
func IconButton(c *ui.Context, icon, label string, disabled bool) ui.Element {
	return iconControl(c, icon, label, false, disabled)
}

// IconToggle provides an accessible selected state along with its tooltip.
func IconToggle(c *ui.Context, icon, label string, selected bool) ui.Element {
	return iconControl(c, icon, label, selected, false)
}

func iconControl(c *ui.Context, icon, label string, selected, disabled bool) ui.Element {
	button := ui.ButtonBase(c).Label(label).Tooltip(label).Size(32, 30).Radius(6).Center().Disabled(disabled)

	if selected {
		button.Selected(true).Background(c.Theme().Selection)
	} else if button.Hovered() && !disabled {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() { Symbol(c, icon, c.Theme().Text) })
	return button
}

func Muted(c *ui.Context, text string) {
	ui.Text(c, text).TextColor(c.Theme().TextMuted).FontSize(12)
}

func Panel(c *ui.Context, children func()) {
	ui.Column(c).FillWidth().Gap(12).Padding(16).Radius(8).
		Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(children)
}

func SectionHeading(c *ui.Context, icon, title string) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		Symbol(c, icon, c.Theme().TextMuted)
		Heading(c, title)
	})
}

type Status struct {
	Text, Icon string
	Color      ui.Color
	Loading    bool
}

func StatusLabel(c *ui.Context, status Status) {
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		if status.Loading {
			ui.Spinner(c.Key("spinner")).Size(16, 16)
		} else if status.Icon != "" {
			Symbol(c.Key("icon"), status.Icon, status.Color)
		}
		ui.Text(c.Key("label"), status.Text).FontSize(12).TextColor(status.Color)
	})
}
