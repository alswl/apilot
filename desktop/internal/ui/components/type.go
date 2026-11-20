package components

import "github.com/egoist/mygo/ui"

func Title(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(17).FontWeight(600)
}

func Heading(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(15).FontWeight(600)
}

func Body(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(15)
}

func DataText(c *ui.Context, text string) ui.Element {
	return ui.Text(c, text).FontSize(13)
}
