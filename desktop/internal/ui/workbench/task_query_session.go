package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type taskQueryBrowsing struct {
	slice         *string
	page          int
	selected      string
	scroll, board ui.ScrollState
}

type taskQueryConversion struct {
	view     savedTaskView
	original string
	revision int64
	ids      []string
}

type taskQueryWorkspace struct {
	returnFocus                                                        string
	revealTab                                                          bool
	conversion                                                         map[string]taskQueryConversion
	comparison                                                         string
	liveRevision                                                       int64
	configConflict                                                     bool
	selectPageEdge                                                     int
	immediate                                                          bool
	autoLimit                                                          int
	path, problem                                                      string
	config                                                             taskQueryConfig
	drafts                                                             map[string]taskQueryView
	browsing                                                           map[string]taskQueryBrowsing
	effective                                                          map[string]common.WorkbenchTaskQuery
	slice                                                              *string
	page                                                               int
	objectIndex                                                        map[string]int
	result                                                             *common.WorkbenchTaskQueryResult
	loaded                                                             string
	cancel                                                             context.CancelFunc
	sequence                                                           uint64
	dayTimer                                                           *time.Timer
	loading, valid                                                     bool
	diagnostic                                                         string
	viewOpen, expressionOpen, conditionOpen, fieldsOpen                bool
	dialog, name, fieldID, conditionField, conditionOp, conditionValue string
	field                                                              *common.WorkbenchQueryField
	fieldPreview                                                       *common.WorkbenchTaskQueryResult
	previewCancel                                                      context.CancelFunc
	previewSequence                                                    uint64
	fieldProblem                                                       string
}

func (m *mockup) activeTaskQuery() *taskQueryView {
	if draft, exists := m.customQuery.drafts[m.taskView]; exists {
		return &draft
	}
	if saved := m.currentSavedView(); saved != nil && saved.Query != nil && saved.Query.Engine == "cel" {
		value := *saved.Query
		return &value
	}
	return nil
}

func (m *mockup) changeTaskQuery(value taskQueryView) {
	q := &m.customQuery
	if q.drafts == nil {
		q.drafts = make(map[string]taskQueryView)
	}
	q.drafts[m.taskView] = value
	q.page = 0
	q.valid = false
}

func (m *mockup) rememberQueryBrowsing() {
	q := &m.customQuery
	if q.browsing == nil {
		q.browsing = make(map[string]taskQueryBrowsing)
	}
	q.browsing[m.taskView] = taskQueryBrowsing{q.slice, q.page, m.taskSelection, m.pageScroll[tasksPage], m.taskBoardScroll}
}

func (m *mockup) restoreQueryBrowsing() {
	q := &m.customQuery
	state := q.browsing[m.taskView]
	q.slice, q.page = state.slice, state.page
	m.taskSelection, m.pageScroll[tasksPage], m.taskBoardScroll = state.selected, state.scroll, state.board
	q.viewOpen, q.expressionOpen, q.conditionOpen, q.fieldsOpen = false, false, false, false
	m.invalidateTaskQuery()
}

func (m *mockup) invalidateTaskQuery() {
	q := &m.customQuery
	q.sequence++
	q.immediate = true
	if q.cancel != nil {
		q.cancel()
		q.cancel = nil
	}
	if q.previewCancel != nil {
		q.previewCancel()
		q.previewCancel = nil
	}
	q.previewSequence++
	q.fieldPreview = nil
	q.result, q.loaded, q.loading, q.valid = nil, "", false, false
	q.diagnostic, q.comparison = "", ""
	if q.dayTimer != nil {
		q.dayTimer.Stop()
		q.dayTimer = nil
	}
}

func (m *mockup) queryInput(view taskQueryView) common.WorkbenchTaskQuery {
	q := &m.customQuery
	input := common.WorkbenchTaskQuery{
		Timezone: q.config.Timezone, Fields: q.config.Fields, Keyword: view.Keyword, Expression: view.Expression,
		Display: view.List, SliceValue: q.slice, Limit: 50,
	}
	input.Display.Layout = "list"
	if m.taskBoard {
		input.Display = view.Board
		input.Display.Layout = "board"
		input.Limit = 100
	} else if m.taskPageSizeOverride > 0 {
		input.Limit = min(m.taskPageSizeOverride, 100)
	} else if q.autoLimit > 0 {
		input.Limit = q.autoLimit
	}
	// Retain projected fields for value suggestions and detail explanations.
	input.Display.Fields = nil
	if input.Display.SliceBy == "" {
		input.SliceValue = nil
	}
	if m.sourceFilter != "全部来源" {
		_, id := m.taskViewSource()
		if id == "" {
			id = "unresolved-scope"
		}
		input.SourceIDs = []string{id}
	}
	switch m.statusFilter {
	case "处理中", "进行中":
		input.States = []string{"in_progress"}
	case "待处理":
		input.States = []string{"open"}
	case "已完成":
		input.States = []string{"done"}
	case "不计划", "不计划处理", "已取消":
		input.States = []string{"cancelled"}
	case "未知":
		input.States = []string{"unknown"}
	case "未完成":
		input.States = []string{"open", "in_progress", "unknown"}
	}
	if m.taskWatchOnly {
		ids := []string{}
		for _, task := range m.objects {
			if m.isWatched(task.ID) {
				ids = append(ids, task.ID)
			}
		}
		slices.Sort(ids)
		input.WatchIDs = &ids
	}
	return input
}

type taskQueryRunner interface {
	QueryWorkbenchTasks(context.Context, common.WorkbenchTaskQuery) (common.WorkbenchTaskQueryResult, error)
}

func loadCustomQuery(ctx context.Context, remote taskQueryRunner, input common.WorkbenchTaskQuery, page int) (common.WorkbenchTaskQueryResult, int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		result, actual, err := readCustomQueryPages(ctx, remote, input, page)
		if err == nil || common.ProtocolError(err).Code != "STALE_REVISION" {
			return result, actual, err
		}
	}
	return common.WorkbenchTaskQueryResult{}, 0, common.Err("STALE_REVISION", "Task snapshot changed")
}

func readCustomQueryPages(ctx context.Context, remote taskQueryRunner, input common.WorkbenchTaskQuery, page int) (common.WorkbenchTaskQueryResult, int, error) {
	result, err := remote.QueryWorkbenchTasks(ctx, input)
	if err != nil {
		return result, 0, err
	}
	input.RequiredRevision, input.AsOfDate, input.PageToken = result.Revision, result.AsOfDate, result.PageToken
	if input.Display.Layout != "board" {
		page = min(page, max(0, (result.SliceTotal-1)/input.Limit))
		if page == 0 {
			return result, page, nil
		}
		input.Offset = page * input.Limit
		result, err = remote.QueryWorkbenchTasks(ctx, input)
		return result, page, err
	}
	for len(result.Items) < min(result.SliceTotal, (page+1)*input.Limit) {
		input.Offset = len(result.Items)
		next, err := remote.QueryWorkbenchTasks(ctx, input)
		if err != nil {
			return common.WorkbenchTaskQueryResult{}, 0, err
		}
		if next.Revision != result.Revision || next.PageToken != result.PageToken || len(next.Items) == 0 {
			return common.WorkbenchTaskQueryResult{}, 0, common.Err("STALE_REVISION", "Task snapshot changed")
		}
		result.Items = append(result.Items, next.Items...)
	}
	return result, min(page, max(0, (result.SliceTotal-1)/input.Limit)), nil
}

func (m *mockup) ensureCustomQuery(view taskQueryView) {
	delay := 120 * time.Millisecond
	if m.customQuery.immediate {
		delay = 0
	}
	m.scheduleCustomQuery(view, delay)
}

func (m *mockup) scheduleCustomQuery(view taskQueryView, delay time.Duration) {
	q := &m.customQuery
	if m.win == nil || m.live == nil || !m.liveLoaded || m.liveStale || m.liveDisconnected || m.liveBusy {
		return
	}
	input := m.queryInput(view)
	input.RequiredRevision = q.liveRevision
	signature, _ := json.Marshal([]any{m.taskView, q.page, input, view})
	key := string(signature)
	if q.loaded == key {
		return
	}
	if q.cancel != nil {
		q.cancel()
	}
	q.loaded, q.loading, q.valid = key, true, false
	q.immediate = false
	q.sequence++
	seq, id, page, window, remote := q.sequence, m.taskView, q.page, m.win, m.live
	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel
	fallback, hadFallback := q.effective[id]
	conversion, converting := q.conversion[id]
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		result, actual, err := loadCustomQuery(ctx, remote, input, page)
		diagnostic, comparison := "", ""
		if err == nil && converting && conversion.revision == result.Revision {
			compare := input
			compare.Display = common.WorkbenchQueryDisplay{Layout: "board"}
			compare.SliceValue, compare.Limit = nil, 100
			all, _, compareErr := loadCustomQuery(ctx, remote, compare, len(conversion.ids)/100+result.QueryTotal/100+1)
			if compareErr != nil || all.Revision != result.Revision {
				err = common.Err("STALE_REVISION", "Task snapshot changed")
			} else {
				comparison = taskQueryComparison(conversion.ids, all.Items)
			}
		}
		if err != nil && common.ProtocolError(err).Code == "INVALID_ARGUMENT" {
			diagnostic = err.Error()
			if hadFallback && fallback.Expression != input.Expression {
				safe := input
				safe.Expression = fallback.Expression
				result, actual, err = loadCustomQuery(ctx, remote, safe, page)
			}
		}
		mygo.RunOnMain(func() {
			if ctx.Err() != nil || window.IsDestroyed() || m.taskView != id || seq != q.sequence {
				return
			}
			q.loading, q.valid = false, err == nil && diagnostic == ""
			q.diagnostic, q.comparison = diagnostic, comparison
			if err != nil {
				q.result = nil
				if diagnostic == "" {
					q.diagnostic = err.Error()
				}
				if common.ProtocolError(err).Code == "STALE_REVISION" {
					m.refreshLive()
				}
			} else {
				q.result, q.page = &result, actual
				accepted, _ := json.Marshal([]any{id, actual, input, view})
				q.loaded = string(accepted)
				if q.valid {
					if q.effective == nil {
						q.effective = make(map[string]common.WorkbenchTaskQuery)
					}
					q.effective[id] = input
				}
				if m.taskSelection != "" && !m.inspector && !slices.ContainsFunc(result.Items, func(item common.WorkbenchQueryItem) bool {
					return item.Task.ID == m.taskSelection
				}) {
					m.taskSelection = ""
				}
				if q.selectPageEdge != 0 {
					rows := m.customQueryRows()
					if len(rows) > 0 {
						index := 0
						if q.selectPageEdge < 0 {
							index = len(rows) - 1
						}
						m.selectTask(rows[index])
					}
					q.selectPageEdge = 0
				}
				m.armQueryCalendar(window, input.Timezone)
			}
			window.Invalidate()
		})
	}()
}

func (m *mockup) armQueryCalendar(window *mygo.Window, tz string) {
	q := &m.customQuery
	zone, err := time.LoadLocation(tz)
	if err != nil {
		return
	}
	now := time.Now().In(zone)
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, zone)
	if q.dayTimer != nil {
		q.dayTimer.Stop()
	}
	q.dayTimer = time.AfterFunc(time.Until(next), func() {
		mygo.RunOnMain(func() {
			if m.win == window && !window.IsDestroyed() {
				m.invalidateTaskQuery()
				window.Invalidate()
			}
		})
	})
}

func (m *mockup) customQueryChanged() bool {
	view := m.activeTaskQuery()
	saved := m.currentSavedView()
	return view != nil && (saved == nil || !reflect.DeepEqual(view, saved.Query) || m.taskViewChanged())
}

func (m *mockup) setTaskQueryProblem(err error) {
	q := &m.customQuery
	switch {
	case err == nil:
		q.problem, q.configConflict = "", false
	case errors.Is(err, errTaskQueryConfigChanged):
		q.configConflict = true
		q.problem = tr("视图配置已在另一窗口更新")
		m.invalidateTaskQuery()
	default:
		q.problem = err.Error()
	}
}

func (m *mockup) saveCustomQuery() {
	q := &m.customQuery
	saved, view := m.currentSavedView(), m.activeTaskQuery()
	conversion, converting := q.conversion[m.taskView]
	if converting && q.comparison != "" {
		saved = &conversion.view
	}
	if saved == nil || view == nil || q.configConflict || !q.valid || q.result == nil || !m.customQueryChanged() {
		return
	}
	update := *saved
	view.Dependencies = slices.Clone(q.result.FieldDependencies)
	update.State, update.Query = m.viewState(), view
	update.State.Page = 0
	err := m.updateTaskQueryConfig(func(cfg *taskQueryConfig) error { return putTaskQueryView(cfg, update) })
	if err == nil {
		delete(q.drafts, m.taskView)
		delete(q.conversion, m.taskView)
	}
	m.setTaskQueryProblem(err)
}

func (m *mockup) newTaskQueryView(name string) savedTaskView {
	return savedTaskView{ID: common.ID(), Name: name, State: defaultTaskViewState(""), Query: &taskQueryView{
		Engine: "cel", Environment: 1,
		List:  common.WorkbenchQueryDisplay{Fields: []string{"task.status", "task.source_id", "task.due"}},
		Board: common.WorkbenchQueryDisplay{Fields: []string{"task.source_id", "task.due"}},
	}}
}

func (m *mockup) queryTabAction(action string) {
	q := &m.customQuery
	q.dialog, q.name = action, ""
	if saved := m.currentSavedView(); saved != nil {
		switch action {
		case "rename", "delete":
			q.name = saved.Name
		case "copy", "convert":
			q.name = fmt.Sprintf("%s %s", saved.Name, tr("副本"))
		}
	}
}

func taskQueryComparison(old []string, items []common.WorkbenchQueryItem) string {
	newIDs := make(map[string]bool, len(items))
	for _, item := range items {
		newIDs[item.Task.ID] = true
	}
	removed, shared := 0, 0
	for _, id := range old {
		if newIDs[id] {
			shared++
		} else {
			removed++
		}
	}
	return fmt.Sprintf(tr("原查询 %d 项，CEL %d 项，新增 %d 项，移除 %d 项"), len(old), len(items), len(items)-shared, removed)
}

func (m *mockup) queryPagePosition() (page, pages int) {
	q := &m.customQuery
	view := m.activeTaskQuery()
	if q.result == nil || view == nil {
		return 0, 1
	}
	size := m.queryInput(*view).Limit
	return q.page, max(1, (q.result.SliceTotal+size-1)/size)
}

func (m *mockup) browseCustomQueryPage(delta int) bool {
	q := &m.customQuery
	page, pages := m.queryPagePosition()
	if q.loading || m.taskBoard || page+delta < 0 || page+delta >= pages {
		return false
	}
	q.page += delta
	q.immediate = true
	m.pageScroll[tasksPage] = ui.ScrollState{}
	return true
}

// Fit the page to the laid-out scroll viewport, retaining the previous first row.
func (m *mockup) fitCustomQueryPage(height float32, grouped bool) bool {
	q := &m.customQuery
	if height <= 0 || m.taskPageSizeOverride > 0 || m.inspector {
		return false
	}
	rowHeight := float32(28)
	if grouped {
		rowHeight += 24
	}
	limit := max(1, min(100, int((height-34)/rowHeight)))
	if limit == q.autoLimit {
		return false
	}
	previous := q.autoLimit
	if previous == 0 {
		previous = 50
	}
	q.page = q.page * previous / limit
	q.autoLimit = limit
	q.immediate = true
	return true
}
