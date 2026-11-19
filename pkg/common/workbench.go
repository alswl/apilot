package common

import "time"

const WorkbenchAPIVersion = "1"
const WorkbenchAPIVersionHeader = "X-Apilot-Workbench-Version"

const (
	WorkbenchGitHubIssues  = "github_issues"
	WorkbenchObsidianTasks = "obsidian_tasks"
)

type WorkbenchTask struct {
	ConnectionVersion int64                   `json:"connection_version,omitempty"`
	TargetVersion     string                  `json:"target_version,omitempty"`
	State             string                  `json:"state,omitempty"`
	Mode              string                  `json:"mode,omitempty"`
	SourceKey         string                  `json:"-"`
	ID                string                  `json:"source_object_id"`
	ConnectionID      string                  `json:"connection_id"`
	NativeID          string                  `json:"native_id"`
	Kind              string                  `json:"kind"`
	Title             string                  `json:"title"`
	Locator           string                  `json:"locator"`
	Assignees         []string                `json:"assignees"`
	Labels            []string                `json:"labels"`
	LabelValues       map[string]string       `json:"label_values"`
	Summary           string                  `json:"summary"`
	Related           []WorkbenchTaskResource `json:"related"`
	Activity          []WorkbenchTaskEvent    `json:"activity"`
	NativeFields      map[string]string       `json:"native_fields"`
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

const WorkbenchObsidianCLI = "obsidian_cli"

type WorkbenchConnection struct {
	ReadMethod       string            `json:"read_method,omitempty"`
	WriteEnabled     bool              `json:"write_enabled,omitempty"`
	BridgeConfigured bool              `json:"bridge_configured,omitempty"`
	Paused           bool              `json:"paused,omitempty"`
	ID               string            `json:"id"`
	Kind             string            `json:"kind"`
	Name             string            `json:"name"`
	Vault            string            `json:"vault,omitempty"`
	Scope            string            `json:"scope,omitempty"`
	Repository       string            `json:"repository,omitempty"`
	Mode             string            `json:"mode"`
	Statuses         map[string]string `json:"statuses,omitempty"`
	Version          int64             `json:"version"`
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

type WorkbenchCapabilities struct {
	Read        bool `json:"read"`
	Refresh     bool `json:"refresh"`
	Create      bool `json:"create"`
	SetState    bool `json:"set_state"`
	ListTargets bool `json:"list_targets"`
	Import      bool `json:"import"`
}

type WorkbenchSnapshot struct {
	Capabilities   WorkbenchCapabilities  `json:"capabilities"`
	PendingWrites  []WorkbenchTaskReceipt `json:"pending_writes,omitempty"`
	PluginDisabled bool                   `json:"plugin_disabled,omitempty"`
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

type WorkbenchTaskWrite struct {
	RequestID         string   `json:"request_id"`
	ConnectionID      string   `json:"connection_id,omitempty"`
	ConnectionVersion int64    `json:"connection_version"`
	SourceObjectID    string   `json:"source_object_id,omitempty"`
	TargetVersion     string   `json:"target_version,omitempty"`
	Title             string   `json:"title,omitempty"`
	State             string   `json:"state,omitempty"`
	NotePath          string   `json:"note_path,omitempty"`
	HeadingPath       []string `json:"heading_path,omitempty"`
}

type WorkbenchTaskReceipt struct {
	RequestID      string `json:"request_id"`
	ConnectionID   string `json:"connection_id"`
	SourceObjectID string `json:"source_object_id,omitempty"`
	SyncState      string `json:"sync_state"`
}

type WorkbenchNote struct {
	NotePath string `json:"note_path"`
}

type WorkbenchHeading struct {
	HeadingPath   []string `json:"heading_path"`
	Level         int      `json:"level"`
	TargetVersion string   `json:"target_version,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// WorkbenchBridgeInput is write-only connection configuration.
type WorkbenchBridgeInput struct {
	Endpoint     string `json:"bridge_endpoint,omitempty"`
	Token        string `json:"bridge_token,omitempty"`
	Certificate  string `json:"bridge_certificate,omitempty"`
	WriteEnabled *bool  `json:"write_enabled,omitempty"`
}
