package components

import "github.com/egoist/mygo/ui"

type InspectorOptions struct {
	Title, DetailID, BackLabel, CloseLabel string
	Width                                  float32
	HeaderActions                          func(*ui.Context)
}

// Inspector hosts the same detail in a fixed-width panel or in the full
// content region. The caller owns selection and navigation.
func Inspector(c *ui.Context, options InspectorOptions, close func(), content func()) {
	column := ui.Column(c).FillHeight().Background(c.Theme().Background).Padding(16).Gap(12)
	if options.Width == 0 {
		column.Grow(1)
	} else {
		column.Width(options.Width).Shrink(0)
	}
	column.Children(func() {
		ui.Row(c.Key("header")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if options.Width == 0 && IconButton(c.Key("back"), "chevron.left", options.BackLabel, false).Clicked() {
				close()
			}
			Muted(c.Key("title"), options.Title)
			ui.Spacer(c.Key("space")).Grow(1)
			if options.HeaderActions != nil {
				options.HeaderActions(c)
			}
			if options.Width != 0 && IconButton(c.Key("close"), "xmark", options.CloseLabel, false).Clicked() {
				close()
			}
		})
		ui.Scroll(c.Key(options.DetailID)).Grow(1).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(12).Children(content)
		})
	})
}
