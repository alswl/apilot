package common

import "time"

const (
	WorkbenchGitHubIssues  = "github_issues"
	WorkbenchObsidianTasks = "obsidian_tasks"
)

type WorkbenchTask struct {
	ID           string                  `json:"source_object_id"`
	ConnectionID string                  `json:"connection_id"`
	NativeID     string                  `json:"native_id"`
	Kind         string                  `json:"kind"`
	Title        string                  `json:"title"`
	Locator      string                  `json:"locator"`
	Assignees    []string                `json:"assignees"`
	Labels       []string                `json:"labels"`
	LabelValues  map[string]string       `json:"label_values"`
	Summary      string                  `json:"summary"`
	Related      []WorkbenchTaskResource `json:"related"`
	Activity     []WorkbenchTaskEvent    `json:"activity"`
	NativeFields map[string]string       `json:"native_fields"`
}

type WorkbenchTaskResource struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	TaskID string `json:"task_id"`
}

type WorkbenchTaskEvent struct {
	At   string    `json:"at"`
	Time time.Time `json:"time"`
	Text string    `json:"text"`
}
