package services

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

type workbenchPreparedRow struct {
	task      queryTask
	source    common.WorkbenchConnection
	connector WorkbenchConnector
}

type workbenchPreparedQuery struct {
	directory      map[string]common.WorkbenchQueryFieldInfo
	sourceVersions map[string]int64
	fields         []common.WorkbenchQueryFieldInfo
	sources        []common.WorkbenchQuerySource
	rows           []workbenchPreparedRow
}

type workbenchQueryProjectionCache struct {
	mu    sync.Mutex
	key   string
	value *workbenchPreparedQuery
}

func readWorkbenchQueryBoundary(ctx context.Context, store workbenchQueryStore) (int64, []common.WorkbenchQueryScope, error) {
	revision, scopes, _, err := store.WorkbenchQuerySnapshot(ctx, true)
	return revision, scopes, err
}

// Reuse projections only on the same authorized boundary and field mappings.
// CEL evaluation, pagination and the final visibility check run on every call.
func (cache *workbenchQueryProjectionCache) load(ctx context.Context, store workbenchQueryStore,
	revision int64, scopes []common.WorkbenchQueryScope, input common.WorkbenchTaskQuery,
	directory map[string]common.WorkbenchQueryFieldInfo, zone *time.Location,
) (*workbenchPreparedQuery, error) {
	key := workbenchQueryToken(common.WorkbenchTaskQuery{Fields: input.Fields, Timezone: input.Timezone}, revision, scopes, "")
	cache.mu.Lock()
	if cache.value != nil && cache.key == key {
		value := cache.value
		cache.mu.Unlock()
		return value, nil
	}
	cache.mu.Unlock()

	current, visible, tasks, err := store.WorkbenchQuerySnapshot(ctx, false)
	if err != nil {
		return nil, err
	}
	if current != revision || !reflect.DeepEqual(visible, scopes) {
		return nil, staleWorkbenchQuery()
	}
	value, err := prepareWorkbenchQuery(ctx, input, directory, zone, visible, tasks)
	if err != nil {
		return nil, err
	}
	if len(tasks) <= 20000 {
		cache.mu.Lock()
		cache.key, cache.value = key, value
		cache.mu.Unlock()
	}
	return value, nil
}

// Copy only the returned page so callers cannot change a cached projection.
func copyWorkbenchQueryItem(item *common.WorkbenchQueryItem) {
	task := &item.Task
	task.Priority = copyWorkbenchQueryPointer(task.Priority)
	task.Assignees = slices.Clone(task.Assignees)
	task.Labels = slices.Clone(task.Labels)
	task.WorkbenchLabels = slices.Clone(task.WorkbenchLabels)
	task.LabelValues = maps.Clone(task.LabelValues)
	task.Related = slices.Clone(task.Related)
	task.Activity = slices.Clone(task.Activity)
	task.NativeFields = maps.Clone(task.NativeFields)
	item.Fields = maps.Clone(item.Fields)
	for key, value := range item.Fields {
		value.Text = copyWorkbenchQueryPointer(value.Text)
		value.Number = copyWorkbenchQueryPointer(value.Number)
		value.Values = slices.Clone(value.Values)
		value.Keys = slices.Clone(value.Keys)
		item.Fields[key] = value
	}
}

func copyWorkbenchQueryPointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
