package components

import "github.com/egoist/mygo/ui"

type ChartBar struct {
	Label, Caption string
	Value          float64
}

// BarChart shows values on a shared zero-based scale, with exact values beside the bars.
func BarChart(c *ui.Context, title string, bars []ChartBar) {
	peak := float64(0)
	for _, bar := range bars {
		peak = max(peak, bar.Value)
	}
	if peak == 0 {
		peak = 1
	}
	ui.Column(c).FillWidth().Gap(10).Children(func() {
		SectionHeading(c.Key("heading"), "chart.bar", title)
		for i, bar := range bars {
			ui.Row(c.Key(i)).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				DataText(c.Key("label"), bar.Label).Width(132).SingleLine().Ellipsis("…").Tooltip(bar.Label)
				ui.Row(c.Key("track")).Grow(1).MinWidth(0).Height(10).Radius(4).Background(c.Theme().SurfaceHover).Children(func() {
					ui.Box(c.Key("bar")).WidthPercent(float32(100 * bar.Value / peak)).FillHeight().Radius(4).Background(c.Theme().Accent)
				})
				DataText(c.Key("caption"), bar.Caption).Width(80).SingleLine().Tooltip(bar.Caption)
			})
		}
	})
}
