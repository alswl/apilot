package workbench

import "github.com/egoist/mygo/ui"

// changeTag keeps annotations out of the fixture and business state.
func (m *mockup) changeTag(c *ui.Context, key, explanation string) {
	if !m.showChangeTags {
		return
	}
	c = c.Key(key)
	ui.Row(c).Gap(8).Children(func() {
		tag := ui.ButtonBase(c.Key("tag")).Label(tr("新增：") + explanation).
			Padding(4).Radius(0).Background(c.Theme().Success)
		tag.Children(func() {
			ui.Text(c, "NEW").Bold().FontSize(11).TextColor(ui.Hex("#ffffff"))
		})
		if tag.Clicked() {
			m.closedTips[key] = !m.closedTips[key]
		} else if tag.Hovered() && !m.hoveredTips[key] {
			m.closedTips[key] = true
		}
		m.hoveredTips[key] = tag.Hovered()
		if !m.closedTips[key] {
			tip := ui.Row(c.Key("tip")).Padding(4).Background(c.Theme().Surface)
			tip.Children(func() {
				ui.Text(c, explanation).FontSize(12).TextColor(c.Theme().TextMuted)
			})
			if tip.Hovered() {
				m.closedTips[key] = true
			}
		}
	})
}
