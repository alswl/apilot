package workbench

import (
	_ "embed"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
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
	color := ui.Hex("#7C3AED")
	if kind == managedGitHub || kind == githubIssues || kind == common.WorkbenchGitHubRepository {
		color = c.Theme().Text
	}
	if kind == common.WorkbenchLocal {
		color = c.Theme().TextMuted
	}
	connectorMark(c, kind, size, color)
}

func connectorMark(c *ui.Context, kind string, size float32, color ui.Color) {
	switch kind {
	case common.WorkbenchLocal:
		ui.Box(c).Size(size, size).Shrink(0).Children(func() { components.Symbol(c, "internaldrive", color) })
	case managedGitHub, githubIssues, common.WorkbenchGitHubRepository:
		ui.Icon(c, githubLogo).Size(size, size).TextColor(color).Shrink(0)
	case obsidianTasks:
		ui.Icon(c, obsidianLogo).Size(size, size).TextColor(color).Shrink(0)
	default:
		components.Symbol(c, "doc.text", color)
	}
}
