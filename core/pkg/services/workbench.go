package services

import "github.com/alswl/apilot/core/pkg/common"

// WorkbenchConnector describes a source kind and its fixture configuration.
type WorkbenchConnector struct {
	Release          *common.WorkbenchConnectorRelease `json:"release,omitempty"`
	PluginID         string                            `json:"plugin_id,omitempty"`
	Capabilities     common.WorkbenchCapabilities      `json:"capabilities"`
	Kind             string                            `json:"kind"`
	Name             string                            `json:"name"`
	UserConfigurable bool                              `json:"user_configurable"`
	DemoCapabilities string                            `json:"-"`
	DemoSyncMode     string                            `json:"-"`
	SearchAliases    []string                          `json:"search_aliases,omitempty"`
	SearchFields     []string                          `json:"search_fields,omitempty"`
}

func WorkbenchConnectors() []WorkbenchConnector {
	return []WorkbenchConnector{
		{Kind: common.WorkbenchLocal, Name: "Local", SearchAliases: []string{"local", "import"}},

		{Kind: common.WorkbenchGitHubRepository, Name: "GitHub", UserConfigurable: true, SearchAliases: []string{"github", "issues", "repo"}, SearchFields: []string{"repo", "assignee", "state", "state_reason"}},
		{
			Kind: common.WorkbenchGitHubIssues, Name: "GitHub",
			DemoCapabilities: "Issues 写入 · Webhook", DemoSyncMode: "定时拉取",
			SearchAliases: []string{"github", "issues"},
			SearchFields:  []string{"repo", "assignee", "milestone", "state", "state_reason"},
		},
		{
			Kind: common.WorkbenchObsidianTasks, Name: "Obsidian Tasks", UserConfigurable: true,
			DemoCapabilities: "文件变化 · 增量读取 · 回写", DemoSyncMode: "文件变化",
			SearchAliases: []string{"obsidian"},
			SearchFields:  []string{"file", "path", "section", "line", "created", "start", "scheduled", "done", "cancelled", "recurrence", "depends_on", "on_completion", "checkbox"},
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
