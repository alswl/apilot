package workbench

import (
	_ "embed"
	"encoding/json"
	"time"

	"github.com/alswl/apilot/core/pkg/common"
)

//go:embed fixtures/workbench.json
var fixtureJSON []byte

//go:embed fixtures/tasks_extended.json
var extendedTaskJSON []byte

type sourceObject = common.WorkbenchTask
type taskResource = common.WorkbenchTaskResource
type taskEvent = common.WorkbenchTaskEvent

type connection struct {
	RateLimitUntil *time.Time
	PluginDisabled bool
	WriteEnabled   bool
	Paused         bool
	ActiveAttempt  int64
	ReadProgress   *common.WorkbenchReadProgress
	Version        int64  `json:"-"`
	ID             string `json:"connection_id"`
	Name           string `json:"name"`

	SourceKind      string            `json:"source_kind"`
	Repository      string            `json:"repository"`
	IssueLabels     []string          `json:"issue_labels"`
	IssueAssignees  []string          `json:"issue_assignees"`
	IssueMilestones []string          `json:"issue_milestones"`
	SearchFields    []string          `json:"search_fields"`
	SearchAliases   []string          `json:"search_aliases"`
	Writable        bool              `json:"writable"`
	ConnectorID     string            `json:"connector_id"`
	Scope           string            `json:"scope"`
	Identity        string            `json:"identity"`
	Capabilities    string            `json:"capabilities"`
	Status          string            `json:"status"`
	LastConfirmedAt string            `json:"last_confirmed_at"`
	StatusLabels    map[string]string `json:"status_labels"`
	CheckboxMapping map[string]string `json:"checkbox_mapping"`
	VaultPath       string            `json:"vault_path"`
	SyncMode        string            `json:"sync_mode"`
}

type mutation struct {
	ID            string            `json:"mutation_id"`
	SourceObject  string            `json:"source_object_id"`
	BaseFields    map[string]string `json:"base_fields"`
	DesiredFields map[string]string `json:"desired_fields"`
	Status        string            `json:"status"`
}

type feedback struct {
	ID              string   `json:"feedback_id"`
	EvidenceRefs    []string `json:"evidence_refs"`
	SubjectIDs      []string `json:"subject_ids"`
	ProposedActions []string `json:"proposed_actions"`
	Status          string   `json:"status"`
}

type scenarioFixture struct {
	Objects     []sourceObject      `json:"objects"`
	Connections []connection        `json:"connections"`
	Feedback    feedback            `json:"feedback"`
	Mutation    mutation            `json:"mutation"`
	Automation  []common.Task       `json:"automation"`
	RunOutcomes map[string][]string `json:"run_outcomes"`
}

func loadFixture() scenarioFixture {
	var data scenarioFixture
	if err := json.Unmarshal(fixtureJSON, &data); err != nil {
		panic("invalid bundled workbench fixture: " + err.Error())
	}
	return data
}

func loadExtendedTasks() []sourceObject {
	var data []sourceObject
	if err := json.Unmarshal(extendedTaskJSON, &data); err != nil {
		panic(err)
	}
	return data
}
