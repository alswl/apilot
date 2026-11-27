package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchQueryStore interface {
	WorkbenchQuerySnapshot(context.Context, bool) (int64, []common.WorkbenchQueryScope, []common.WorkbenchTask, error)
}

func (s *Service) QueryWorkbenchTasks(ctx context.Context, input common.WorkbenchTaskQuery) (common.WorkbenchTaskQueryResult, error) {
	return s.queryWorkbenchTasks(ctx, input, time.Now)
}

func (s *Service) queryWorkbenchTasks(ctx context.Context, input common.WorkbenchTaskQuery, now func() time.Time) (common.WorkbenchTaskQueryResult, error) {
	var empty common.WorkbenchTaskQueryResult
	store, ok := s.store.(workbenchQueryStore)
	if !ok {
		return empty, common.Err("INTERNAL", "Workbench query storage unavailable")
	}
	zone, err := time.LoadLocation(input.Timezone)
	if err != nil || input.Timezone == "" || input.Timezone == "Local" {
		return empty, common.Err("INVALID_ARGUMENT", "An IANA workspace timezone is required")
	}
	if err := validateWorkbenchQuery(input); err != nil {
		return empty, err
	}
	directory, err := workbenchQueryFields(input.Fields)
	if err != nil {
		return empty, err
	}
	program, err := s.workbenchQueryPrograms.compile(input.Expression, input.Fields)
	if err != nil {
		return empty, err
	}
	calendar := workbenchQueryCalendar(now(), zone)
	date := calendar["today"].(string)
	if input.AsOfDate != "" && input.AsOfDate != date {
		return empty, staleWorkbenchQuery()
	}
	revision, sources, tasks, err := store.WorkbenchQuerySnapshot(ctx, false)
	if err != nil {
		return empty, err
	}
	if input.RequiredRevision != 0 && input.RequiredRevision != revision {
		return empty, staleWorkbenchQuery()
	}
	token := workbenchQueryToken(input, revision, sources, date)
	if input.PageToken != "" && input.PageToken != token {
		return empty, staleWorkbenchQuery()
	}

	result, queryErr := calculateWorkbenchQuery(ctx, input, program, directory, zone, calendar, sources, tasks)
	latest, currentSources, _, err := store.WorkbenchQuerySnapshot(ctx, true)
	if err != nil {
		return empty, err
	}
	if latest != revision || now().In(zone).Format(time.DateOnly) != date ||
		workbenchQueryToken(input, latest, currentSources, date) != token {
		return empty, staleWorkbenchQuery()
	}
	if queryErr != nil {
		return empty, queryErr
	}
	result.Revision, result.AsOfDate, result.PageToken = revision, date, token
	return result, nil
}

func staleWorkbenchQuery() error {
	return common.Err("STALE_REVISION", "Task snapshot, workspace date, or query settings changed. Restart at offset zero.")
}

func validateWorkbenchQuery(input common.WorkbenchTaskQuery) error {
	if input.Limit < 0 || input.Limit > 100 || input.Offset < 0 || input.RequiredRevision < 0 || utf8.RuneCountInString(input.Expression) > 16384 {
		return common.Err("INVALID_ARGUMENT", "Invalid task query limits")
	}
	if input.Offset > 0 && (input.RequiredRevision == 0 || input.AsOfDate == "" || input.PageToken == "") {
		return common.Err("INVALID_ARGUMENT", "Continuation requires required_revision, as_of_date and page_token")
	}
	if input.AsOfDate != "" {
		if _, err := time.Parse(time.DateOnly, input.AsOfDate); err != nil {
			return common.Err("INVALID_ARGUMENT", "as_of_date must use YYYY-MM-DD")
		}
	}
	for _, state := range input.States {
		if !validWorkbenchState(state) {
			return common.Err("INVALID_ARGUMENT", "Invalid task status: "+state)
		}
	}
	for _, source := range input.SourceIDs {
		if source == "" || strings.TrimSpace(source) != source {
			return common.Err("INVALID_ARGUMENT", "Source constraints require stable scope IDs")
		}
	}
	return nil
}

func workbenchQueryCalendar(now time.Time, zone *time.Location) map[string]any {
	year, month, day := now.In(zone).Date()
	// Use UTC for date arithmetic after resolving the current local calendar day.
	today := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	week := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	return map[string]any{
		"today": today.Format(time.DateOnly), "tomorrow": today.AddDate(0, 0, 1).Format(time.DateOnly),
		"week_start": week.Format(time.DateOnly), "next_week_start": week.AddDate(0, 0, 7).Format(time.DateOnly),
		"seven_days_end": today.AddDate(0, 0, 7).Format(time.DateOnly),
	}
}

func workbenchQueryToken(input common.WorkbenchTaskQuery, revision int64, sources []common.WorkbenchQueryScope, date string) string {
	input.Limit, input.Offset, input.RequiredRevision, input.AsOfDate, input.PageToken = 0, 0, 0, "", ""
	raw, _ := json.Marshal([]any{workbenchQueryEnvironment, input, revision, sources, date})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func calculateWorkbenchQuery(ctx context.Context, input common.WorkbenchTaskQuery, program workbenchQueryProgram,
	directory map[string]common.WorkbenchQueryFieldInfo, zone *time.Location, calendar map[string]any,
	sources []common.WorkbenchQueryScope, tasks []common.WorkbenchTask,
) (common.WorkbenchTaskQueryResult, error) {
	result := common.WorkbenchTaskQueryResult{FieldDependencies: append([]string{}, program.dependencies...)}
	sourceByID := make(map[string]common.WorkbenchConnection, len(sources))
	for _, source := range sources {
		sourceByID[source.ID] = source.WorkbenchConnection
	}
	connectors := make(map[string]WorkbenchConnector)
	for _, connector := range WorkbenchConnectors() {
		connectors[connector.Kind] = connector
	}
	connectors["github-issues"] = WorkbenchConnector{SearchAliases: connectors[common.WorkbenchGitHubRepository].SearchAliases}
	// The catalog reflects the visible summary snapshot, independent of the query
	// and sample constraints. Field capabilities never require a detail read.
	catalog := make(map[string]map[string]common.WorkbenchQueryFieldInfo)
	for _, source := range sources {
		catalog[source.ID] = make(map[string]common.WorkbenchQueryFieldInfo)
		for _, key := range connectors[source.Kind].SearchFields {
			catalog[source.ID][key] = queryFieldInfo("native.", key, key, workbenchSearchFieldType(key))
		}
	}
	nativeByTask := make([]map[string]queryNativeValue, len(tasks))
	for i, task := range tasks {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if _, visible := sourceByID[task.ConnectionID]; !visible {
			continue
		}
		nativeByTask[i] = workbenchQueryNative(task, connectors[task.Kind])
		for key, raw := range nativeByTask[i] {
			fields := catalog[task.ConnectionID]
			if existing, ok := fields[key]; !ok || (existing.Type != raw.kind && raw.reason == "") {
				kind := raw.kind
				if ok {
					kind = "text"
				}
				fields[key] = queryFieldInfo("native.", key, key, kind)
			}
		}
	}
	result.Sources = make([]common.WorkbenchQuerySource, 0, len(sources))
	for _, source := range sources {
		info := common.WorkbenchQuerySource{ID: source.ID, Name: source.Name, NativeFields: []common.WorkbenchQueryFieldInfo{}}
		for _, key := range slices.Sorted(maps.Keys(catalog[source.ID])) {
			field := catalog[source.ID][key]
			info.NativeFields = append(info.NativeFields, field)
			if existing, found := directory[field.ID]; found && existing.Type != field.Type {
				field = queryFieldInfo("native.", key, key, "text")
			}
			directory[field.ID] = field
		}
		result.Sources = append(result.Sources, info)
	}
	for _, id := range slices.Sorted(maps.Keys(directory)) {
		result.Fields = append(result.Fields, directory[id])
	}
	display, err := validateWorkbenchQueryDisplay(input.Display, input.SliceValue, directory)
	if err != nil {
		return result, err
	}

	keyword := strings.ToLower(strings.TrimSpace(input.Keyword))
	matches := make([]common.WorkbenchQueryItem, 0)
	constraints := newWorkbenchQueryConstraints(input)
	for i, task := range tasks {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		source, visible := sourceByID[task.ConnectionID]
		if !visible || !constraints.match(task) {
			continue
		}
		native := nativeByTask[i]
		for key, descriptor := range catalog[source.ID] {
			if _, found := native[key]; !found {
				native[key] = queryNativeValue{kind: descriptor.Type}
			}
		}
		projected := projectWorkbenchQueryTask(task, source, native, input.Fields, zone)
		if !workbenchQueryKeyword(keyword, projected, source, connectors[task.Kind]) {
			continue
		}
		match, err := program.match(ctx, projected, input.Fields, calendar)
		if err != nil {
			return result, err
		}
		if match {
			matches = append(matches, projected.item)
		}
	}
	aggregateWorkbenchQuery(&result, matches, display, input)
	for i := range result.Items {
		task := &result.Items[i].Task
		task.TargetVersion = workbenchTaskTargetVersion(*task)
		task.ConnectionVersion = sourceByID[task.ConnectionID].Version
	}
	return result, ctx.Err()
}

type workbenchQueryConstraints struct {
	sources, states, watch, tasks map[string]bool
}

func newWorkbenchQueryConstraints(input common.WorkbenchTaskQuery) workbenchQueryConstraints {
	set := func(values []string) map[string]bool {
		result := make(map[string]bool, len(values))
		for _, value := range values {
			result[value] = true
		}
		return result
	}
	constraints := workbenchQueryConstraints{sources: set(input.SourceIDs), states: set(input.States)}
	if input.WatchIDs != nil {
		constraints.watch = set(*input.WatchIDs)
	}
	if input.TaskIDs != nil {
		constraints.tasks = set(*input.TaskIDs)
	}
	return constraints
}

func (c workbenchQueryConstraints) match(task common.WorkbenchTask) bool {
	state := task.State
	if !validWorkbenchState(state) {
		state = "unknown"
	}
	return (len(c.sources) == 0 || c.sources[task.ConnectionID]) &&
		(len(c.states) == 0 || c.states[state]) &&
		(c.watch == nil || c.watch[task.ID]) && (c.tasks == nil || c.tasks[task.ID])
}

func workbenchQueryKeyword(keyword string, task queryTask, source common.WorkbenchConnection, connector WorkbenchConnector) bool {
	if keyword == "" {
		return true
	}
	t := task.item.Task
	values := []string{t.Title, t.NativeID, t.Locator, source.ID, source.Name, source.Kind, connector.Name}
	values = append(values, connector.SearchAliases...)
	values = append(values, task.item.Fields["task.labels"].Values...)
	values = append(values, t.Assignees...)
	for _, value := range task.native {
		values = append(values, value)
	}
	return slices.ContainsFunc(values, func(value string) bool {
		return strings.Contains(strings.ToLower(value), keyword)
	})
}
