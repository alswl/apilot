package services

import "github.com/alswl/apilot/pkg/common"

// WorkbenchConnector describes a source kind and its fixture configuration.
type WorkbenchConnector struct {
	Kind             string
	Name             string
	DemoCapabilities string
	DemoSyncMode     string
	SearchAliases    []string
	SearchFields     []string
}

func WorkbenchConnectors() []WorkbenchConnector {
	return []WorkbenchConnector{
		{
			Kind: common.WorkbenchGitHubIssues, Name: "GitHub Issues",
			DemoCapabilities: "Issues 写入 · Webhook", DemoSyncMode: "定时拉取",
			SearchAliases: []string{"github", "issues"},
			SearchFields:  []string{"repo", "assignee", "milestone", "state", "state_reason"},
		},
		{
			Kind: common.WorkbenchObsidianTasks, Name: "Obsidian Tasks",
			DemoCapabilities: "文件变化 · 增量读取 · 回写", DemoSyncMode: "文件变化",
			SearchAliases: []string{"obsidian"},
			SearchFields:  []string{"path", "section", "start", "scheduled", "done", "checkbox"},
		},
	}
}

// ApplyWorkbenchTaskFields applies the edit delta while preserving unrelated fields.
func ApplyWorkbenchTaskFields(task *common.WorkbenchTask, before, after map[string]string) {
	for key, value := range after {
		if value != before[key] {
			if task.NativeFields == nil {
				task.NativeFields = make(map[string]string)
			}
			task.NativeFields[key] = value
		}
	}
	for key := range before {
		if _, kept := after[key]; !kept {
			delete(task.NativeFields, key)
		}
	}
}
