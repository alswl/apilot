package services

import (
	"cmp"
	"math/big"
	"slices"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
)

var workbenchQueryStatuses = []string{"open", "in_progress", "done", "cancelled", "unknown"}

func validateWorkbenchQueryDisplay(display common.WorkbenchQueryDisplay, slice *string, directory map[string]common.WorkbenchQueryFieldInfo) (common.WorkbenchQueryDisplay, error) {
	if display.Layout == "" {
		display.Layout = "list"
	}
	if display.Layout == "board" && display.ColumnBy == "" {
		display.ColumnBy = "task.status"
	}
	switch display.Layout {
	case "list":
		if display.SwimlaneBy != "" || display.ColumnBy != "" {
			return display, common.Err("INVALID_ARGUMENT", "Columns and swimlanes require board layout")
		}
	case "board":
		if display.GroupBy != "" || display.ColumnBy == display.SwimlaneBy {
			return display, common.Err("INVALID_ARGUMENT", "Board columns and swimlanes must use different fields")
		}
	default:
		return display, common.Err("INVALID_ARGUMENT", "Unknown task query layout")
	}
	for _, key := range []string{display.GroupBy, display.ColumnBy, display.SwimlaneBy, display.SliceBy} {
		if key != "" && !directory[key].Group {
			return display, common.Err("INVALID_ARGUMENT", "Field cannot group tasks: "+key)
		}
	}
	if display.SortBy == "" {
		display.SortBy = "task.title"
	}
	if !directory[display.SortBy].Sort {
		return display, common.Err("INVALID_ARGUMENT", "Field cannot sort tasks: "+display.SortBy)
	}
	if display.SumBy != "" && !directory[display.SumBy].Sum {
		return display, common.Err("INVALID_ARGUMENT", "Field cannot sum tasks: "+display.SumBy)
	}
	if slice != nil && display.SliceBy == "" {
		return display, common.Err("INVALID_ARGUMENT", "A slice value requires a slice field")
	}
	seen := make(map[string]bool)
	for _, key := range display.Fields {
		if !directory[key].Display || seen[key] {
			return display, common.Err("INVALID_ARGUMENT", "Invalid or duplicate display field: "+key)
		}
		seen[key] = true
	}
	return display, nil
}

type workbenchQueryGroup struct {
	common.WorkbenchQueryGroup
	sum big.Int
}

type workbenchQueryGroups map[string]*workbenchQueryGroup

func (groups workbenchQueryGroups) add(value common.WorkbenchQueryValue, number common.WorkbenchQueryValue) string {
	key, name := "", ""
	if value.Reason == "" && value.Text != nil {
		key, name = value.Key, *value.Text
	}
	group := groups[key]
	if group == nil {
		group = &workbenchQueryGroup{WorkbenchQueryGroup: common.WorkbenchQueryGroup{Key: key, Name: name, Missing: key == ""}}
		groups[key] = group
	}
	if name < group.Name {
		group.Name = name
	}
	group.Count++
	if number.Number != nil {
		group.Valid++
		queryAddDecimal(&group.sum, number.Decimal)
	}
	return key
}

func (groups workbenchQueryGroups) sorted(stateOrder, includeEmpty bool) []common.WorkbenchQueryGroup {
	if includeEmpty {
		for _, state := range workbenchQueryStatuses {
			if _, exists := groups[state]; !exists {
				groups[state] = &workbenchQueryGroup{WorkbenchQueryGroup: common.WorkbenchQueryGroup{Key: state, Name: state}}
			}
		}
	}
	out := make([]common.WorkbenchQueryGroup, 0, len(groups))
	for _, group := range groups {
		if group.Valid != 0 {
			sum := queryFormatDecimal(&group.sum)
			group.Sum = &sum
		}
		out = append(out, group.WorkbenchQueryGroup)
	}
	slices.SortFunc(out, func(a, b common.WorkbenchQueryGroup) int {
		switch {
		case a.Missing && !b.Missing:
			return 1
		case b.Missing && !a.Missing:
			return -1
		case stateOrder:
			return cmp.Compare(slices.Index(workbenchQueryStatuses, a.Key), slices.Index(workbenchQueryStatuses, b.Key))
		default:
			return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.Key, b.Key))
		}
	})
	return out
}

func aggregateWorkbenchQuery(result *common.WorkbenchTaskQueryResult, matches []common.WorkbenchQueryItem, display common.WorkbenchQueryDisplay, input common.WorkbenchTaskQuery) {
	result.QueryTotal = len(matches)
	slicesByValue, columns, lanes := make(workbenchQueryGroups), make(workbenchQueryGroups), make(workbenchQueryGroups)
	cells := make(map[[2]string]int)
	columnBy := cmp.Or(display.ColumnBy, display.GroupBy)
	selected := matches[:0]
	for _, item := range matches {
		if display.SliceBy != "" {
			key := slicesByValue.add(item.Fields[display.SliceBy], common.WorkbenchQueryValue{})
			if input.SliceValue != nil && *input.SliceValue != key {
				continue
			}
		}
		if columnBy != "" {
			item.Column = columns.add(item.Fields[columnBy], item.Fields[display.SumBy])
		}
		if display.SwimlaneBy != "" {
			item.Lane = lanes.add(item.Fields[display.SwimlaneBy], common.WorkbenchQueryValue{})
		}
		if display.Layout == "board" {
			cells[[2]string{item.Lane, item.Column}]++
		}
		selected = append(selected, item)
	}
	result.SliceTotal = len(selected)
	result.Slices = slicesByValue.sorted(display.SliceBy == "task.status", false)
	result.Columns = columns.sorted(columnBy == "task.status", display.ColumnBy == "task.status")
	result.Lanes = lanes.sorted(display.SwimlaneBy == "task.status", false)
	laneOrder, columnOrder := workbenchGroupPositions(result.Lanes), workbenchGroupPositions(result.Columns)
	compareCell := func(laneA, columnA, laneB, columnB string) int {
		return cmp.Or(cmp.Compare(laneOrder[laneA], laneOrder[laneB]), cmp.Compare(columnOrder[columnA], columnOrder[columnB]))
	}
	result.Cells = make([]common.WorkbenchQueryCell, 0, len(cells))
	for cell, count := range cells {
		result.Cells = append(result.Cells, common.WorkbenchQueryCell{Lane: cell[0], Column: cell[1], Count: count})
	}
	slices.SortFunc(result.Cells, func(a, b common.WorkbenchQueryCell) int {
		return compareCell(a.Lane, a.Column, b.Lane, b.Column)
	})
	slices.SortFunc(selected, func(a, b common.WorkbenchQueryItem) int {
		if group := compareCell(a.Lane, a.Column, b.Lane, b.Column); group != 0 {
			return group
		}
		return cmp.Or(compareWorkbenchQueryValues(a.Fields[display.SortBy], b.Fields[display.SortBy], display.Descending),
			strings.Compare(a.Task.ID, b.Task.ID))
	})
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	start := min(input.Offset, len(selected))
	result.Items = selected[start : start+min(limit, len(selected)-start)]
	for i := range result.Items {
		item := &result.Items[i]
		fields := display.Fields
		if len(fields) == 0 {
			continue
		}
		projected := map[string]common.WorkbenchQueryValue{"task.title": item.Fields["task.title"]}
		for _, id := range fields {
			value, exists := item.Fields[id]
			if !exists {
				value.Reason = "unmapped"
			}
			projected[id] = value
		}
		item.Fields = projected
	}
}

func workbenchGroupPositions(groups []common.WorkbenchQueryGroup) map[string]int {
	positions := make(map[string]int, len(groups))
	for i, group := range groups {
		positions[group.Key] = i
	}
	return positions
}

func compareWorkbenchQueryValues(a, b common.WorkbenchQueryValue, descending bool) int {
	presentA := a.Text != nil || a.Number != nil || len(a.Values) != 0
	presentB := b.Text != nil || b.Number != nil || len(b.Values) != 0
	switch {
	case !presentA && !presentB:
		return 0
	case !presentA:
		return 1
	case !presentB:
		return -1
	}
	var order int
	switch {
	case a.Number != nil && b.Number != nil:
		order = cmp.Compare(*a.Number, *b.Number)
	case a.Number != nil:
		order = -1
	case b.Number != nil:
		order = 1
	default:
		order = strings.Compare(workbenchQuerySortText(a), workbenchQuerySortText(b))
	}
	if descending {
		return -order
	}
	return order
}

func workbenchQuerySortText(value common.WorkbenchQueryValue) string {
	if value.Text != nil {
		return strings.ToLower(*value.Text)
	}
	return strings.ToLower(strings.Join(value.Values, ", "))
}
