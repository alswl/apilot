package workbench

import (
	"fmt"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

func (m *mockup) customQueryRows() []int {
	result := m.customQuery.result
	if result == nil {
		return nil
	}
	rows := make([]int, 0, len(result.Items))
	for _, item := range result.Items {
		if i, exists := m.customQuery.objectIndex[item.Task.ID]; exists {
			rows = append(rows, i)
		}
	}
	return rows
}

func (m *mockup) customQueryResults(c *ui.Context, view taskQueryView) {
	q := &m.customQuery
	result := q.result
	if result == nil {
		if q.diagnostic != "" && ui.Button(c.Key("retry"), tr("重试")).Clicked() {
			m.invalidateTaskQuery()
		}
		return
	}
	display := view.List
	if m.taskBoard {
		display = view.Board
	}
	ui.Row(c).Fill().MinHeight(0).MinWidth(0).Gap(10).Children(func() {
		if display.SliceBy != "" {
			m.customQuerySlices(c.Key("slices"), result)
		}
		ui.Column(c.Key("content")).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Gap(6).Children(func() {
			if result.QueryTotal == 0 && len(m.objects) == 0 {
				m.liveTaskEmptyState(c.Key("empty-workspace"))
			} else if result.SliceTotal == 0 {
				components.Muted(c.Key("empty"), tr("没有符合条件的任务"))
			}
			if m.taskBoard {
				m.customQueryBoard(c.Key("board"), result, display, view)
				if len(result.Items) < result.SliceTotal && ui.Button(c.Key("more"), tr("加载更多")).Disabled(q.loading).Clicked() {
					q.page++
					q.immediate = true
				}
			} else {
				m.customQueryList(c.Key("list"), result, display, view)
			}
		})
	})
}

func queryGroupTitle(group common.WorkbenchQueryGroup) string {
	if group.Missing {
		return tr("未设置")
	}
	switch group.Key {
	case "open", "in_progress", "done", "cancelled", "unknown":
		if group.Name == group.Key {
			return tr(workbenchStateLabel(group.Key))
		}
	}
	return group.Name
}

func (m *mockup) customQuerySlices(c *ui.Context, result *common.WorkbenchTaskQueryResult) {
	q := &m.customQuery
	ui.Scroll(c).Width(116).MinWidth(116).Shrink(0).FillHeight().Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			options := []components.ViewItem{{ID: "all", Title: fmt.Sprintf("%s · %d", tr("全部任务"), result.QueryTotal)}}
			current := "all"
			for _, group := range result.Slices {
				options = append(options, components.ViewItem{ID: "slice:" + group.Key, Title: fmt.Sprintf("%s · %d", queryGroupTitle(group), group.Count)})
			}
			if q.slice != nil {
				current = "slice:" + *q.slice
			}
			for _, option := range options {
				button := ui.ButtonBase(c.Key(option.ID)).Label(option.Title).Tooltip(option.Title).
					Selected(option.ID == current).FillWidth().MinWidth(0).Padding(5, 6)
				if option.ID == current {
					button.Background(c.Theme().Selection)
				}
				button.Children(func() { components.DataText(c, option.Title).SingleLine().Ellipsis("…") })
				if button.Clicked() {
					q.slice = nil
					if value, ok := strings.CutPrefix(option.ID, "slice:"); ok {
						q.slice = &value
					}
					q.page = 0
					q.immediate = true
					m.taskBoardScroll, m.pageScroll[tasksPage] = ui.ScrollState{}, ui.ScrollState{}
				}
			}
		})
	})
}

func (m *mockup) customQueryList(c *ui.Context, result *common.WorkbenchTaskQueryResult, display common.WorkbenchQueryDisplay, view taskQueryView) {
	q := &m.customQuery
	scroll := ui.ScrollBoth(c.Key("scroll")).Grow(1).MinHeight(0).MinWidth(0).FillWidth().TrackScroll(&m.pageScroll[tasksPage])
	scroll.Children(func() {
		ui.Column(c).FillWidth().Gap(4).Children(func() {
			if len(result.Items) == 0 {
				return
			}
			ui.Row(c.Key("header")).Gap(8).Children(func() {
				components.Heading(c.Key("title"), tr("任务")).Width(m.queryListFieldWidth(view, "task.title", 210))
				for _, id := range display.Fields {
					field := queryInfo(result.Fields, id)
					if ui.Button(c.Key(id), queryFieldTitle(field)).Width(m.queryListFieldWidth(view, id, 104)).Disabled(!field.Sort).Clicked() {
						view.List.Descending = view.List.SortBy == id && !view.List.Descending
						view.List.SortBy = id
						m.changeTaskQuery(view)
					}
				}
			})
			var previous *string
			for _, item := range result.Items {
				if display.GroupBy != "" && (previous == nil || *previous != item.Column) {
					for _, group := range result.Columns {
						if group.Key == item.Column {
							components.Heading(c.Key("group:"+group.Key), queryColumnHeading(group, display.SumBy != ""))
							break
						}
					}
					key := item.Column
					previous = &key
				}
				m.customQueryItem(c.Key(item.Task.ID), item, display.Fields, view, false)
			}
		})
	})
	if m.fitCustomQueryPage(scroll.LayoutBounds().H, display.GroupBy != "") {
		c.Invalidate()
	}
	limit := m.queryInput(view).Limit
	pages := max(1, (result.SliceTotal+limit-1)/limit)
	ui.Row(c.Key("pagination")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		if components.IconButton(c.Key("previous"), "chevron.left", tr("上一页"), q.page == 0 || q.loading).Clicked() && q.page > 0 {
			m.browseCustomQueryPage(-1)
		}
		components.Muted(c.Key("page"), fmt.Sprintf("%d / %d", q.page+1, pages))
		if components.IconButton(c.Key("next"), "chevron.right", tr("下一页"), q.page+1 >= pages || q.loading).Clicked() && q.page+1 < pages {
			m.browseCustomQueryPage(1)
		}
	})
}

func queryColumnHeading(group common.WorkbenchQueryGroup, sum bool) string {
	label := fmt.Sprintf("%s · %d", queryGroupTitle(group), group.Count)
	if sum {
		value := "—"
		if group.Sum != nil {
			value = *group.Sum
		}
		label += " · " + value
	}
	return label
}

func (m *mockup) customQueryBoard(c *ui.Context, result *common.WorkbenchTaskQueryResult, display common.WorkbenchQueryDisplay, view taskQueryView) {
	cells := make(map[[2]string][]common.WorkbenchQueryItem)
	for _, item := range result.Items {
		key := [2]string{item.Lane, item.Column}
		cells[key] = append(cells[key], item)
	}
	ui.ScrollBoth(c).Grow(1).MinHeight(0).MinWidth(0).FillWidth().TrackScroll(&m.taskBoardScroll).Children(func() {
		ui.Column(c).Gap(8).Children(func() {
			ui.Row(c.Key("headers")).Gap(8).Children(func() {
				for _, column := range result.Columns {
					components.Heading(c.Key(column.Key), queryColumnHeading(column, display.SumBy != "")).Width(225)
				}
			})
			lanes := result.Lanes
			if display.SwimlaneBy == "" {
				lanes = []common.WorkbenchQueryGroup{{}}
			}
			for _, lane := range lanes {
				ui.Column(c.Key(lane.Key)).Gap(5).Children(func() {
					if display.SwimlaneBy != "" {
						components.Muted(c.Key("lane"), fmt.Sprintf("%s · %d", queryGroupTitle(lane), lane.Count))
					}
					ui.Row(c.Key("cells")).Gap(8).AlignItems(ui.Start).Children(func() {
						for _, column := range result.Columns {
							ui.Column(c.Key(column.Key)).Width(225).Gap(5).Padding(7).Background(c.Theme().Background).Children(func() {
								for _, item := range cells[[2]string{lane.Key, column.Key}] {
									m.customQueryItem(c.Key(item.Task.ID), item, display.Fields, view, true)
								}
							})
						}
					})
				})
			}
		})
	})
}

func queryInfo(fields []common.WorkbenchQueryFieldInfo, id string) common.WorkbenchQueryFieldInfo {
	for _, field := range fields {
		if field.ID == id {
			return field
		}
	}
	return common.WorkbenchQueryFieldInfo{ID: id, Name: id}
}

func (m *mockup) customQueryItem(c *ui.Context, item common.WorkbenchQueryItem, fields []string, view taskQueryView, board bool) {
	index, available := m.customQuery.objectIndex[item.Task.ID]
	selected := m.taskSelection == item.Task.ID
	theme := c.Theme()
	row := ui.Row(c).Gap(8).AlignItems(ui.Center)
	if board {
		background := theme.Surface
		if selected {
			background = theme.Selection
		}
		row.Wrap().FillWidth().Padding(8).Gap(5).Radius(6).Border(1, theme.Border).Background(background)
	}
	row.Children(func() {
		title := ui.ButtonBase(c.Key("title")).Label(item.Task.Title).Tooltip(item.Task.Title).
			Justify(ui.Start).MinWidth(0).MinHeight(24).Selected(selected).Disabled(!available)
		if board {
			title.FillWidth()
		} else {
			title.Width(m.queryListFieldWidth(view, "task.title", 210))
		}
		if selected && !board {
			title.Background(theme.Selection)
		} else if title.Hovered() {
			title.Background(theme.SurfaceHover)
		}
		title.Children(func() {
			label := components.DataText(c, item.Task.Title).MinWidth(0).SingleLine().Ellipsis("…")
			if board {
				label.FontWeight(600)
			}
		})
		if m.restoreTaskFocus && m.taskSelection == item.Task.ID {
			title.Focus(ui.FocusOptions{PreventScroll: !m.layout.revealSelection})
			if m.layout.revealSelection {
				title.ScrollIntoView()
			}
			m.layout.revealSelection = false
			m.restoreTaskFocus = false
		}
		if title.Clicked() && available {
			m.taskSelection = item.Task.ID
			m.openFullTask(index)
		}
		for _, id := range fields {
			value := item.Fields[id]
			text := queryValueText(value)
			if id == "task.status" && value.Key != "" {
				text = tr(workbenchStateLabel(value.Key))
			}
			if value.Reason != "" {
				if !board {
					components.DataText(c.Key(id), text).TextColor(theme.TextMuted).Width(m.queryListFieldWidth(view, id, 104))
				}
				continue
			}
			field := queryInfo(m.customQuery.result.Fields, id)
			label := queryFieldTitle(field) + " · " + text
			button := ui.ButtonBase(c.Key(id)).Label(label).Tooltip(label).MinHeight(23).MinWidth(0).Justify(ui.Start)
			if !board {
				button.Width(m.queryListFieldWidth(view, id, 104))
			}
			if button.Hovered() {
				button.Background(theme.SurfaceHover)
			}
			button.Children(func() {
				components.DataText(c, text).TextColor(theme.TextMuted).MinWidth(0).SingleLine().Ellipsis("…")
			})
			if button.Clicked() {
				op, raw := "等于", text
				if value.Key != "" {
					raw = value.Key
				}
				if field.Type == "multi_select" && len(value.Values) > 0 {
					op, raw = "包含", value.Values[0]
					if len(value.Keys) > 0 {
						raw = value.Keys[0]
					}
				}
				if condition, err := taskQueryCondition(field, op, raw); err == nil {
					view.addCondition(queryFieldTitle(field)+" = "+text, condition)
					m.changeTaskQuery(view)
				}
			}
		}
	})
}

func (m *mockup) queryListFieldWidth(view taskQueryView, id string, fallback float32) float32 {
	if width := view.Widths[id]; width > 0 {
		return width
	}
	return fallback
}
