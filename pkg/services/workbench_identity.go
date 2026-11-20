package services

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/alswl/apilot/pkg/common"
	"github.com/alswl/apilot/pkg/integrations"
)

func matchWorkbench(c common.WorkbenchConnection, before, after []common.WorkbenchTask) {
	prior := map[string][]common.WorkbenchTask{}
	groups := map[string][]int{}
	for _, task := range before {
		key, _, _ := strings.Cut(task.SourceKey, "\x00")
		prior[key] = append(prior[key], task)
	}
	for i := range after {
		groups[after[i].SourceKey] = append(groups[after[i].SourceKey], i)
	}
	for key, indexes := range groups {
		previous := prior[key]
		for ordinal, i := range indexes {
			task := &after[i]
			task.ConnectionID, task.Kind, task.Mode = c.ID, c.Kind, c.Mode
			task.SourceKey = fmt.Sprintf("%s\x00%d", key, ordinal)
		}
		if c.Kind == common.WorkbenchObsidianTasks && len(previous) == len(indexes) && len(indexes) > 1 &&
			matchObsidianTasksBySection(previous, after, indexes) {
			continue
		}
		reuse := len(previous) == 1 && len(indexes) == 1
		if len(previous) == len(indexes) && len(indexes) > 1 {
			reuse = true
			for _, i := range indexes {
				task := after[i]
				matched := false
				for _, p := range previous {
					if task.SourceKey == p.SourceKey {
						task.ID = p.ID
						if c.Kind == common.WorkbenchObsidianTasks {
							task.Activity = p.Activity
						}
						matched = reflect.DeepEqual(task, p)
						break
					}
				}
				if !matched {
					reuse = false
					break
				}
			}
		}
		for _, i := range indexes {
			task := &after[i]
			task.ID = common.ID()
			if reuse {
				for _, p := range previous {
					if p.SourceKey == task.SourceKey {
						task.ID = p.ID
						break
					}
				}
			}
		}
	}
}

func matchObsidianTasksBySection(before, after []common.WorkbenchTask, indexes []int) bool {
	type position struct{ id, heading, summary string }
	key := func(t common.WorkbenchTask) position {
		return position{t.NativeID, t.NativeFields["heading_path"], integrations.WorkbenchObsidianWithoutCompletionDate(t.Summary)}
	}
	previous := make(map[position]common.WorkbenchTask, len(before))
	for _, task := range before {
		identity := key(task)
		if _, duplicate := previous[identity]; duplicate {
			return false
		}
		previous[identity] = task
	}
	seen := make(map[position]bool, len(indexes))
	for _, i := range indexes {
		identity := key(after[i])
		if _, ok := previous[identity]; !ok || seen[identity] {
			return false
		}
		seen[identity] = true
	}
	for _, i := range indexes {
		task := &after[i]
		prior := previous[key(*task)]
		task.ID, task.Activity = prior.ID, prior.Activity
	}
	return true
}

// Keep existing IDs when a previously unrecognized attribute changes the
// parser's key for an otherwise identical line.
func recordObsidianImport(c common.WorkbenchConnection, before, after []common.WorkbenchTask, now time.Time) {
	if c.Kind != common.WorkbenchObsidianTasks {
		return
	}
	type oldLine struct{ locator, summary string }
	previous := make(map[string]common.WorkbenchTask, len(before))
	legacy := make(map[oldLine]string)
	for _, task := range before {
		previous[task.ID] = task
		if task.NativeFields["file"] == "" {
			legacy[oldLine{task.Locator, task.Summary}] = task.ID
		}
	}
	assigned := make(map[string]bool, len(after))
	for _, task := range after {
		assigned[task.ID] = true
	}
	for i := range after {
		task := &after[i]
		if _, ok := previous[task.ID]; !ok {
			if id := legacy[oldLine{task.Locator, task.Summary}]; id != "" && !assigned[id] {
				assigned[id], task.ID = true, id
			}
		}
		task.Activity = previous[task.ID].Activity
		if len(task.Activity) == 0 {
			task.Activity = []common.WorkbenchTaskEvent{{Time: now, Text: "从 Obsidian 导入"}}
		}
	}
}
