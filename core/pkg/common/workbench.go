package common

import "time"

const WorkbenchAPIVersion = "1"
const WorkbenchAPIVersionHeader = "X-Apilot-Workbench-Version"

const (
	WorkbenchGitHubIssues     = "github_issues"
	WorkbenchGitHubRepository = "github_repository"
	WorkbenchObsidianTasks    = "obsidian_tasks"
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
	WorkbenchLabels   []WorkbenchLabel        `json:"workbench_labels,omitempty"`
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
	ReadMethod       string            `json:"read_method,omitempty" hidden:"true"`
	WriteEnabled     bool              `json:"write_enabled,omitempty"`
	BridgeConfigured bool              `json:"bridge_configured,omitempty" hidden:"true"`
	Paused           bool              `json:"paused,omitempty"`
	ID               string            `json:"id"`
	Kind             string            `json:"kind"`
	Name             string            `json:"name"`
	Vault            string            `json:"vault,omitempty"`
	Scope            string            `json:"scope,omitempty"`
	Repository       string            `json:"repository,omitempty"`
	AccountLogin     string            `json:"account_login,omitempty"`
	AccessState      string            `json:"access_state,omitempty"`
	WriteDenied      bool              `json:"write_denied,omitempty"`
	Mode             string            `json:"mode"`
	Statuses         map[string]string `json:"statuses,omitempty"`
	Version          int64             `json:"version"`
}

type WorkbenchReadProgress struct {
	Read  int `json:"read"`
	Total int `json:"total"`
}

// WorkbenchNoteObservation describes evidence obtained for one source location.
// A missing location does not establish that a task was deleted or not moved.
type WorkbenchNoteObservation struct {
	Path     string
	Coverage string
	Tasks    []WorkbenchTask
}

// WorkbenchReadResult is internal evidence for one connection read. Its scope
// and coverage project to WorkbenchAttempt; source tasks and errors stay in
// the connector/service boundary and are not themselves an API response.
type WorkbenchReadResult struct {
	ConnectionID      string
	ConnectionVersion int64
	SnapshotVersion   int64
	Scope             string
	Coverage          string
	Paths             []string
	Notes             []WorkbenchNoteObservation
	Tasks             []WorkbenchTask
	Retry             bool
	Err               error
}

const (
	WorkbenchReadFull  = "full"
	WorkbenchReadNotes = "notes"

	WorkbenchReadComplete   = "complete"
	WorkbenchReadPartial    = "partial"
	WorkbenchReadUnverified = "unverified"
)

const (
	WorkbenchCoverageRead    = "read"
	WorkbenchCoverageMissing = "missing"
	WorkbenchCoverageOutside = "outside"
	WorkbenchCoverageFailed  = "failed"
)

type WorkbenchAttempt struct {
	Progress          *WorkbenchReadProgress `json:"progress,omitempty"`
	SyncType          string                 `json:"sync_type,omitempty"`
	RequestCount      int                    `json:"request_count,omitempty"`
	PageCount         int                    `json:"page_count,omitempty"`
	FirstPageMS       int64                  `json:"first_page_ms,omitempty"`
	Scope             string                 `json:"scope,omitempty" enum:"full,notes"`
	Paths             []string               `json:"paths,omitempty"`
	Coverage          string                 `json:"coverage,omitempty" enum:"complete,partial,unverified"`
	ConnectionVersion int64                  `json:"connection_version,omitempty"`
	SnapshotVersion   int64                  `json:"snapshot_version,omitempty"`
	ID                int64                  `json:"id"`
	Trigger           string                 `json:"trigger"`
	State             string                 `json:"state"`
	Started           time.Time              `json:"started"`
	Finished          *time.Time             `json:"finished,omitempty"`
	Error             string                 `json:"error,omitempty"`
	Added             int                    `json:"added"`
	Updated           int                    `json:"updated"`
	Removed           int                    `json:"removed"`
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
	GitHubAccountID int64                  `json:"-"`
	SnapshotVersion int64                  `json:"-"`
	Capabilities    WorkbenchCapabilities  `json:"capabilities"`
	PendingWrites   []WorkbenchTaskReceipt `json:"pending_writes,omitempty"`
	PluginDisabled  bool                   `json:"plugin_disabled,omitempty"`
	RateLimitUntil  *time.Time             `json:"rate_limit_until,omitempty"`
	WorkbenchConnection
	LastSuccess *time.Time         `json:"last_success,omitempty"`
	Attempts    []WorkbenchAttempt `json:"attempts"`
	Tasks       []WorkbenchTask    `json:"-"`
}

type WorkbenchQuery struct {
	LabelID    string `json:"label_id,omitempty"`
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
	Body              string   `json:"body,omitempty"`
	State             string   `json:"state,omitempty"`
	NotePath          string   `json:"note_path,omitempty"`
	HeadingPath       []string `json:"heading_path,omitempty"`
}

type WorkbenchTaskReceipt struct {
	RequestID      string `json:"request_id"`
	ConnectionID   string `json:"connection_id"`
	SourceObjectID string `json:"source_object_id,omitempty"`
	SyncState      string `json:"sync_state"`
	Reason         string `json:"reason,omitempty"`
}

// Operation phases describe request processing, independently of task state.
const (
	WorkbenchOperationAccepted    = "accepted"
	WorkbenchOperationDispatching = "dispatching"
	WorkbenchOperationNeedsReview = "needs_review"
	WorkbenchOperationConfirmed   = "confirmed"
	WorkbenchOperationConflict    = "conflict"
	WorkbenchOperationNotExecuted = "not_executed"
)

// WorkbenchTaskOperation contains the durable input and processing stage.
// The stage is separate from the last confirmed state of the source task.
type WorkbenchTaskOperation struct {
	Input       WorkbenchTaskWrite         `json:"input"`
	Receipt     WorkbenchTaskReceipt       `json:"receipt"`
	Phase       string                     `json:"phase"`
	Fingerprint string                     `json:"-"`
	Binding     *WorkbenchOperationBinding `json:"-"`
}

type WorkbenchOperationBinding struct {
	RepositoryID int64          `json:"repository_id,omitempty"`
	AccountID    int64          `json:"account_id,omitempty"`
	IssueID      int64          `json:"issue_id,omitempty"`
	IssueNumber  int64          `json:"issue_number,omitempty"`
	Vault        string         `json:"vault"`
	Scope        string         `json:"scope"`
	Baseline     *WorkbenchTask `json:"baseline,omitempty"`
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
	Endpoint     string `json:"bridge_endpoint,omitempty" hidden:"true"`
	Token        string `json:"bridge_token,omitempty" hidden:"true"`
	Certificate  string `json:"bridge_certificate,omitempty" hidden:"true"`
	WriteEnabled *bool  `json:"write_enabled,omitempty"`
}

// WorkbenchLabel is a locally managed task label.
type WorkbenchLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// WorkbenchAnnotation is a locally managed note attached to a task.
type WorkbenchAnnotation struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// WorkbenchGitHubProgress is the durable authorization and sync state of a repository.
type WorkbenchGitHubProgress struct {
	ConnectionID  string
	RepositoryID  int64
	AccountID     int64
	AccountLogin  string
	CredentialRef string
	AccessState   string
	WriteDenied   bool
	Cursor        *time.Time
	LastFullSync  *time.Time
	NextPollAt    *time.Time
}
