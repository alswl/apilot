package services

import (
	"context"
	"maps"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/core/pkg/integrations"
)

func observeWorkbenchConnection(ctx context.Context, c common.WorkbenchConnection, version int64, before []common.WorkbenchTask, paths []string, allowNotes bool, read workbenchReader) common.WorkbenchReadResult {
	result := common.WorkbenchReadResult{ConnectionID: c.ID, ConnectionVersion: c.Version, SnapshotVersion: version,
		Scope: common.WorkbenchReadFull, Coverage: common.WorkbenchReadUnverified}
	if len(paths) > 0 && allowNotes {
		result.Scope = common.WorkbenchReadNotes
		result.Paths = append([]string(nil), paths...)
		for _, path := range paths {
			observation, err := integrations.ObserveWorkbenchObsidianNote(ctx, c, path)
			result.Notes = append(result.Notes, observation)
			if err != nil {
				result.Scope = common.WorkbenchReadFull
				break
			}
			updates, needsFull := reconcileWorkbenchObservedNote(c, before, observation)
			if needsFull {
				result.Scope = common.WorkbenchReadFull
				break
			}
			result.Tasks = append(result.Tasks, updates...)
		}
		if result.Scope == common.WorkbenchReadNotes {
			result.Coverage = common.WorkbenchReadComplete
			return result
		}
	}
	result.Paths = nil
	result.Tasks, result.Err = read(ctx, c)
	if result.Err != nil {
		return result
	}
	if c.Kind == common.WorkbenchObsidianTasks {
		firstMissing := missingWorkbenchTaskIDs(c, before, result.Tasks)
		if len(firstMissing) > 0 {
			timer := time.NewTimer(250 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				result.Err = ctx.Err()
			case <-timer.C:
				result.Tasks, result.Err = read(ctx, c)
			}
			if result.Err != nil {
				return result
			}
			if !maps.Equal(firstMissing, missingWorkbenchTaskIDs(c, before, result.Tasks)) {
				result.Retry = true
				result.Err = common.Err("CONFLICT", "Source changed while verifying missing tasks")
				return result
			}
		}
	}
	result.Coverage = common.WorkbenchReadComplete
	return result
}

// reconcileWorkbenchObservedNote computes only updates that can be identified
// from an existing complete snapshot. Additions, removals and ambiguity require
// a full-scope read to rule out moves or duplicate identities elsewhere.
func reconcileWorkbenchObservedNote(c common.WorkbenchConnection, before []common.WorkbenchTask, observation common.WorkbenchNoteObservation) ([]common.WorkbenchTask, bool) {
	if observation.Coverage != common.WorkbenchCoverageRead {
		return nil, true
	}
	var prior []common.WorkbenchTask
	for _, task := range before {
		if task.NativeFields["path"] == observation.Path {
			prior = append(prior, task)
		}
	}
	if len(prior) != len(observation.Tasks) {
		return nil, true
	}
	used := map[string]bool{}
	updates := make([]common.WorkbenchTask, 0, len(prior))
	for _, candidate := range observation.Tasks {
		var matched *common.WorkbenchTask
		matches := 0
		for i := range before {
			old := &before[i]
			if candidate.NativeID != "" {
				if old.NativeID != candidate.NativeID {
					continue
				}
			} else {
				original, current := *old, candidate
				original.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(original.Summary)
				current.Summary = integrations.WorkbenchObsidianWithoutCompletionDate(current.Summary)
				if !integrations.MatchesWorkbenchObsidianTask(original, current) {
					continue
				}
			}
			matches++
			matched = old
		}
		if matches != 1 || matched.NativeFields["path"] != observation.Path || used[matched.ID] {
			return nil, true
		}
		used[matched.ID] = true
		candidate.ID, candidate.ConnectionID, candidate.Kind, candidate.Mode = matched.ID, c.ID, c.Kind, c.Mode
		candidate.SourceKey, candidate.Activity = matched.SourceKey, matched.Activity
		updates = append(updates, candidate)
	}
	return updates, false
}
