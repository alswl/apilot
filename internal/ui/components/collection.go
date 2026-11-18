package components

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

type ListItem struct {
	Title, Subtitle, Icon string
	Status                Status
	Selected              bool
}

// ListRow renders the selection target. Callers place any independent actions
// beside it so that activating them does not select the row.
func ListRow(c *ui.Context, item ListItem) bool {
	labels := []string{item.Title}
	for _, value := range []string{item.Subtitle, item.Status.Text} {
		if value != "" {
			labels = append(labels, value)
		}
	}
	entry := ui.ButtonBase(c).Label(strings.Join(labels, ", ")).Tooltip(item.Title).
		Selected(item.Selected).Radius(6).Justify(ui.Start).Grow(1).Padding(8)
	if item.Selected {
		entry.Background(c.Theme().Selection).TextColor(c.Theme().Text)
	} else if entry.Hovered() {
		entry.Background(c.Theme().SurfaceHover)
	}
	entry.Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			if item.Icon != "" {
				Symbol(c.Key("icon"), item.Icon, c.Theme().Accent)
			}
			ui.Column(c.Key("summary")).Grow(1).MinWidth(0).Gap(4).Children(func() {
				DataText(c.Key("title"), item.Title).Ellipsis("…")
				if item.Subtitle != "" {
					ui.Text(c.Key("subtitle"), item.Subtitle).FontSize(12).Ellipsis("…").TextColor(c.Theme().TextMuted)
				}
			})
			if item.Status.Text != "" {
				StatusLabel(c.Key("status"), item.Status)
			}
		})
	})
	return entry.Clicked()
}

type TableRow struct {
	ID, Title, Icon string
	Cells, Icons    []string
	Leading         func(*ui.Context)
	Menu            func(*ui.Menu)
}

type TableInteraction struct {
	SelectedID  string
	ActivatedID string
}

const (
	tableCellMinHeight      = 20
	TableGroupHeadingHeight = 20
)

// TableHeights returns the header and row heights used by DataTable.
func TableHeights(theme *ui.Theme) (header, row float32) {
	header = theme.Space(8)
	return header, max(header, tableCellMinHeight+2*theme.Space(1.5))
}

// DataTable leaves row order and filtering to the caller. Arrow keys move
// the selection. A double-click or Return opens the selected row.
func DataTable(c *ui.Context, state *ui.ListState, columns []ui.TableColumn, rows []TableRow, selectedID string, revealSelection bool) TableInteraction {
	picked := -1
	state.Key = func(i int) any { return rows[i].ID }
	state.Label = func(i int) string { return rows[i].Title }
	for i, row := range rows {
		if row.ID == selectedID {
			picked = i
			break
		}
	}
	if revealSelection && picked >= 0 {
		state.ScrollIntoView(picked)
	}
	state.Selected = &picked
	var activated string
	headerHeight, rowHeight := TableHeights(c.Theme())
	table := ui.Table(c, state, columns, len(rows), func(i, j int) {
		row := rows[i]
		cell := ui.Row(c).FillWidth().MinHeight(tableCellMinHeight).Gap(6).AlignItems(ui.Center)
		if row.Menu != nil {
			cell.ContextMenu(row.Menu)
		}
		cell.Children(func() {
			if j == 0 && row.Leading != nil {
				row.Leading(c.Key("leading"))
			} else if j == 0 && row.Icon != "" {
				color := c.Theme().TextMuted
				if i == picked {
					color = c.Theme().AccentText
				}
				Symbol(c.Key("icon"), row.Icon, color)
			}
			if j < len(row.Icons) && row.Icons[j] != "" {
				Symbol(c.Key("column-icon"), row.Icons[j], c.Theme().TextMuted)
			}
			if j < len(row.Cells) {
				value := row.Cells[j]
				DataText(c.Key("value"), value).SingleLine().Ellipsis("…").Tooltip(value)
			}
		})
		if revealSelection && i == picked && j == 0 {
			cell.ScrollIntoView()
		}
		if cell.RowClicked() {
			picked = i
		}
		if cell.RowDoubleClicked() {
			activated = row.ID
		}
	}).FillWidth().Height(headerHeight + float32(len(rows))*rowHeight).Clip()
	result := TableInteraction{ActivatedID: activated}
	if picked >= 0 && picked < len(rows) {
		result.SelectedID = rows[picked].ID
		if table.Submitted() {
			result.ActivatedID = result.SelectedID
		}
	}
	if activated != "" {
		result.SelectedID = activated
	}
	return result
}

type TableGroup struct {
	ID, Title string
	Rows      []TableRow
}

type GroupedTableInteraction struct {
	TableInteraction
	Focused bool
}

func GroupedTables(c *ui.Context, ungrouped *ui.ListState, states map[string]*ui.ListState, columns []ui.TableColumn, groups []TableGroup, selected string, restoreFocus, revealSelection bool) GroupedTableInteraction {
	var result GroupedTableInteraction
	for i, group := range groups {
		state := ungrouped
		if group.ID != "" {
			state = states[group.ID]
			if state == nil {
				state = &ui.ListState{}
				states[group.ID] = state
			}
			state.Sort = ungrouped.Sort
			Heading(c.Key(group.ID+"-heading"), group.Title).Height(TableGroupHeadingHeight).SingleLine().Ellipsis("…").Tooltip(group.Title)
		}
		value := DataTable(c.Key(group.ID+"-table"), state, columns, group.Rows, selected, restoreFocus || revealSelection)
		if value.SelectedID != "" && (result.SelectedID == "" || state.FocusWithin(c)) {
			result.SelectedID = value.SelectedID
		}
		if value.ActivatedID != "" {
			result.TableInteraction = value
		}
		if restoreFocus && !result.Focused && (value.SelectedID != "" || (selected == "" && i == 0)) {
			result.Focused = state.Focus(c)
		}
	}
	return result
}
