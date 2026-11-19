package services

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/alswl/apilot/pkg/common"
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
		reuse := len(previous) == 1 && len(indexes) == 1
		if len(previous) == len(indexes) && len(indexes) > 1 {
			reuse = true
			for _, i := range indexes {
				task := after[i]
				matched := false
				for _, p := range previous {
					if task.SourceKey == p.SourceKey {
						task.ID = p.ID
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
