package common

import "time"

const (
	WorkbenchGitHubIssues  = "github_issues"
	WorkbenchObsidianTasks = "obsidian_tasks"
)

type WorkbenchTask struct {
	State        string                  `json:"state,omitempty"`
	Mode         string                  `json:"mode,omitempty"`
	SourceKey    string                  `json:"-"`
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

type WorkbenchConnection struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Name       string            `json:"name"`
	Vault      string            `json:"vault,omitempty"`
	Scope      string            `json:"scope,omitempty"`
	Repository string            `json:"repository,omitempty"`
	Mode       string            `json:"mode"`
	Statuses   map[string]string `json:"statuses,omitempty"`
	Version    int64             `json:"version"`
}

type WorkbenchAttempt struct {
	ID       int64      `json:"id"`
	Trigger  string     `json:"trigger"`
	State    string     `json:"state"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	Error    string     `json:"error,omitempty"`
	Added    int        `json:"added"`
	Updated  int        `json:"updated"`
	Removed  int        `json:"removed"`
}

type WorkbenchSnapshot struct {
	WorkbenchConnection
	LastSuccess *time.Time         `json:"last_success,omitempty"`
	Attempts    []WorkbenchAttempt `json:"attempts"`
	Tasks       []WorkbenchTask    `json:"-"`
}

type WorkbenchQuery struct {
	Connection string `json:"connection"`
	State      string `json:"state"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	Revision   int64  `json:"revision"`
}

type WorkbenchPage struct {
	Items    []WorkbenchTask `json:"items"`
	Total    int             `json:"total"`
	Revision int64           `json:"revision"`
}

const (
	WorkbenchLocal          = "local"
	DefaultWorkbenchLocalID = "local"
)

type WorkbenchImport struct {
	Name  string                `json:"name,omitempty"`
	Tasks []WorkbenchImportTask `json:"tasks"`
}

type WorkbenchImportTask struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	State   string `json:"state"`
	Summary string `json:"summary,omitempty"`
}
