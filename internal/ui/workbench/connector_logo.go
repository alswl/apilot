package workbench

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// The GitHub and Obsidian marks are bundled from Simple Icons 16.0.0.
// https://github.com/simple-icons/simple-icons/tree/16.0.0/icons

//go:embed assets/github.svg
var githubMark []byte

//go:embed assets/obsidian.svg
var obsidianMark []byte

var githubLogo = parseConnectorLogo(githubMark)
var obsidianLogo = parseConnectorLogo(obsidianMark)

func parseConnectorLogo(mark []byte) *ui.SVG {
	logo, err := ui.ParseSVG(mark)
	if err != nil {
		panic("invalid bundled connector logo: " + err.Error())
	}
	return logo
}

func connectorLogo(c *ui.Context, kind string, size float32) {
	logo, color := obsidianLogo, ui.Hex("#7C3AED")
	if kind == githubIssues {
		logo, color = githubLogo, c.Theme().Text
	}
	ui.Icon(c, logo).Size(size, size).TextColor(color).Shrink(0)
}
