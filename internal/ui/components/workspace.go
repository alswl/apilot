package components

import "github.com/egoist/mygo/ui"

type ViewItem struct {
	ID, Title string
}

func ViewStrip(c *ui.Context, items []ViewItem, active string) string {
	var chosen string
	ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
		for _, item := range items {
			entry := ui.ButtonBase(c.Key(item.ID)).Label(item.Title).Selected(active == item.ID).
				Padding(6, 10).MinHeight(28).Radius(6)
			color := c.Theme().TextMuted
			if active == item.ID {
				entry.Background(c.Theme().SurfaceHover)
				color = c.Theme().Text
			} else if entry.Hovered() {
				entry.Background(c.Theme().SurfaceHover)
			}
			entry.Children(func() { DataText(c, item.Title).TextColor(color) })
			if entry.Clicked() {
				chosen = item.ID
			}
		}
	})
	return chosen
}

// ContentSection gives independently readable parts of a detail view a common hierarchy.
func ContentSection(c *ui.Context, title string, content func()) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Divider(c)
		Heading(c, title)
		content()
	})
}

func PropertyRow(c *ui.Context, name, value string) {
	ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
		ui.Text(c, name).FontSize(12).TextColor(c.Theme().TextMuted).Width(90)
		DataText(c, value).Grow(1).MinWidth(0)
	})
}

func DocumentTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(22).FontWeight(600)
}

func ActivityRow(c *ui.Context, timestamp, description string) {
	ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
		Muted(c, timestamp)
		DataText(c, description)
	})
}

// TextField combines a visible label with the input's accessible name.
func TextField(c *ui.Context, label string, value *string) ui.Element {
	var input ui.Element
	ui.Field(c, label, func() {
		input = ui.TextInput(c.Key("input"), value).Label(label).FillWidth()
	})
	return input
}

type Property struct {
	ID, Title, Value string
	Wide             bool
}

// PropertyGrid keeps short values together and gives long references their own row.
func PropertyGrid(c *ui.Context, properties []Property, columns int) {
	for first := 0; first < len(properties); {
		last := first + 1
		if !properties[first].Wide {
			for last < len(properties) && last-first < columns && !properties[last].Wide {
				last++
			}
		}
		ui.Row(c.Key(properties[first].ID + "-row")).FillWidth().Gap(16).Children(func() {
			for _, property := range properties[first:last] {
				ui.Column(c.Key(property.ID)).Grow(1).MinWidth(0).Gap(4).Children(func() {
					Muted(c, property.Title)
					DataText(c, property.Value).Tooltip(property.Value)
				})
			}
		})
		first = last
	}
}

func Tag(c *ui.Context, value string) {
	ui.Row(c).Padding(3, 6).Radius(4).Background(c.Theme().SurfaceHover).Children(func() {
		DataText(c, value).Tooltip(value)
	})
}

func LabelChoices(c *ui.Context, labels []string, selected func(string) bool, toggle func(string)) {
	ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
		for _, label := range labels {
			if ui.Button(c.Key(label), label).Selected(selected(label)).Clicked() {
				toggle(label)
			}
		}
	})
}

func KeyValueTag(c *ui.Context, key, value string) {
	ui.Row(c).Padding(3, 6).Radius(4).Gap(4).AlignItems(ui.Center).
		Background(c.Theme().SurfaceHover).Tooltip(key + ": " + value).Children(func() {
		DataText(c.Key("key"), key).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…")
		DataText(c.Key("value"), value).SingleLine().Ellipsis("…").FontWeight(600)
	})
}
