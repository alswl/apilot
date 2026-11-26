package workbench

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

const defaultTaskPageSize = 10

type taskPageCriteria struct {
	view, query, status, source, sourceID, grouping string
	sort                                            ui.SortOrder
}

func (m *mockup) paginationCriteria() taskPageCriteria {
	return taskPageCriteria{m.taskView, m.search, m.statusFilter, m.sourceFilter, m.sourceFilterID, m.taskGrouping, m.taskSort}
}

func (m *mockup) taskPages(total int) int {
	return max(1, (total+m.taskPageSize-1)/m.taskPageSize)
}

func (m *mockup) syncTaskPage(total int) {
	criteria := m.paginationCriteria()
	if criteria != m.taskPageCriteria {
		m.taskPageIndex = 0
		m.taskPageCriteria = criteria
		m.pageScroll[tasksPage].ScrollTo(0, 0, ui.Animation{})
	}
	m.taskPageIndex = max(0, min(m.taskPageIndex, m.taskPages(total)-1))
}

func (m *mockup) pagedTaskRows(rows []int) []int {
	m.syncTaskPage(len(rows))
	first := m.taskPageIndex * m.taskPageSize
	return rows[first:min(first+m.taskPageSize, len(rows))]
}

func (m *mockup) fitTaskPage(theme *ui.Theme, rows []int, height float32) {
	header, rowHeight := components.TableHeights(theme)
	size := max(1, int((height-header)/rowHeight))
	if m.taskGrouping != "不分组" {
		for size > 1 && !m.groupedPagesFit(rows, size, height, header, rowHeight) {
			size--
		}
	}
	if size == m.taskPageSize {
		return
	}
	criteriaChanged := m.paginationCriteria() != m.taskPageCriteria
	m.syncTaskPage(len(rows))
	anchor := m.taskPageIndex * m.taskPageSize
	if !criteriaChanged {
		if selection := slices.IndexFunc(rows, func(i int) bool { return m.objects[i].ID == m.taskSelection }); selection >= 0 {
			anchor = selection
		}
	}
	m.taskPageSize, m.taskPageIndex = size, anchor/size
	m.layout.revealSelection = m.taskSelection != ""
	m.pageScroll[tasksPage].ScrollTo(0, 0, ui.Animation{})
}

func (m *mockup) groupedPagesFit(rows []int, size int, height, header, rowHeight float32) bool {
	for start := 0; start < len(rows); start += size {
		page := rows[start:min(start+size, len(rows))]
		groups, previous := 1, m.taskGroup(m.objects[page[0]])
		for _, index := range page[1:] {
			group := m.taskGroup(m.objects[index])
			if group != previous {
				groups++
				previous = group
			}
		}
		used := float32(len(page))*rowHeight + float32(groups)*(header+components.TableGroupHeadingHeight+2*taskResultsGap) - taskResultsGap
		if used > height {
			return false
		}
	}
	return true
}

func (m *mockup) setTaskPage(page int) {
	if m.unifiedTasks() {
		m.closeInspector()
	}
	m.taskPageIndex = page
	m.taskSelection = ""
	m.restoreTaskFocus = true
	m.pageScroll[tasksPage].ScrollTo(0, 0, ui.Animation{})
}

func (m *mockup) browseTaskPage(delta int) {
	total := len(m.taskRows())
	m.syncTaskPage(total)
	page := max(0, min(m.taskPageIndex+delta, m.taskPages(total)-1))
	if page != m.taskPageIndex {
		m.setTaskPage(page)
	}
}

func (m *mockup) locateTaskPage(rows []int, index int) bool {
	m.syncTaskPage(len(rows))
	position := slices.Index(rows, index)
	if position < 0 {
		return false
	}
	page := position / m.taskPageSize
	if page != m.taskPageIndex {
		m.taskPageIndex = page
		m.pageScroll[tasksPage].ScrollTo(0, 0, ui.Animation{})
	}
	return true
}

func (m *mockup) taskPagination(c *ui.Context) {
	total := len(m.taskRows())
	m.syncTaskPage(total)
	pageCount := m.taskPages(total)
	summary := tr("共 0 项")
	if total > 0 {
		first := m.taskPageIndex*m.taskPageSize + 1
		last := min(first+m.taskPageSize-1, total)
		summary = fmt.Sprintf(tr("第 %d–%d 项 · 共 %d 项"), first, last, total)
	}
	ui.Divider(c)
	ui.Row(c).FillWidth().Wrap().AlignItems(ui.Center).Gap(8).Children(func() {
		components.Muted(c, summary)
		ui.Spacer(c).Grow(1)
		components.Muted(c, tr("每页"))
		options := []string{"10", "20", "50", "100"}
		size := strconv.Itoa(m.taskPageSize)
		automatic := tr("自动")
		if m.unifiedTasks() {
			options = append([]string{automatic}, options...)
			if m.taskPageSizeOverride == 0 {
				size = automatic
			}
		}
		if ui.Select(c.Key("page-size"), &size, options).Label(tr("每页数量")).MinWidth(0).Width(90).Changed() {
			m.taskPageSizeOverride, _ = strconv.Atoi(size)
			m.taskPageSize = m.taskPageSizeOverride
			if m.taskPageSize == 0 {
				m.taskPageSize = defaultTaskPageSize
			}
			m.setTaskPage(0)
			c.Invalidate()
		}
		if components.IconButton(c.Key("previous"), "chevron.left", tr("上一页"), m.taskPageIndex == 0).Clicked() {
			m.browseTaskPage(-1)
		}
		components.Muted(c, fmt.Sprintf(tr("第 %d / %d 页"), m.taskPageIndex+1, pageCount))
		if components.IconButton(c.Key("next"), "chevron.right", tr("下一页"), m.taskPageIndex+1 >= pageCount).Clicked() {
			m.browseTaskPage(1)
		}
	})
}
