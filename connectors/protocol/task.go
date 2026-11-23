package protocol

import "encoding/json"

const (
	FieldsDescribe = "fields.describe"
	TaskDetail     = "tasks.detail"
	EditMetadata   = "tasks.edit_metadata"
	Mutate         = "tasks.mutate"
	Reconcile      = "tasks.reconcile"
)

type FieldDefinition struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Type     FieldType `json:"type"`
	Writable bool      `json:"writable"`
	Options  []Option  `json:"options,omitempty"`
}

type MappingRule struct {
	Field  string            `json:"field"`
	Write  bool              `json:"write,omitempty"`
	Values map[string]string `json:"values,omitempty"`
}

type FieldCatalog struct {
	Fields   []FieldDefinition      `json:"fields"`
	Defaults map[string]MappingRule `json:"defaults"`
}

type DetailParams struct {
	ScopeContext
	SourceID string `json:"source_id"`
	Number   string `json:"number,omitempty"`
	Segment  string `json:"segment"`
	Page     string `json:"page,omitempty"`
}

type DetailItem struct {
	ID    string    `json:"id"`
	Label string    `json:"label"`
	URL   string    `json:"url,omitempty"`
	Text  *RichText `json:"text,omitempty"`
}

type DetailPage struct {
	Version  string                `json:"version"`
	Fields   map[string]FieldValue `json:"fields,omitempty"`
	Items    []DetailItem          `json:"items,omitempty"`
	NextPage string                `json:"next_page,omitempty"`
}

func Value(kind FieldType, value any) FieldValue {
	data, _ := json.Marshal(value)
	return FieldValue{Type: kind, Value: data}
}

type EditMetadataResult struct {
	Fields  []FieldDefinition `json:"fields"`
	Actions []StateAction     `json:"actions"`
	Create  bool              `json:"create"`
}

type ConnectorEditMetadataResult struct {
	EditMetadataResult
	Observation *DetailPage `json:"observation,omitempty"`
}

type StateAction struct {
	ID       string            `json:"id"`
	Label    string            `json:"label"`
	Required []FieldDefinition `json:"required,omitempty"`
}

type MutationParams struct {
	ScopeContext
	OperationID     string                `json:"operation_id"`
	SourceID        string                `json:"source_id,omitempty"`
	Number          string                `json:"number,omitempty"`
	ExpectedVersion string                `json:"expected_version,omitempty"`
	Baseline        map[string]FieldValue `json:"baseline,omitempty"`
	Patch           map[string]FieldValue `json:"patch,omitempty"`
	Action          string                `json:"action,omitempty"`
	ActionFields    map[string]FieldValue `json:"action_fields,omitempty"`
	Create          map[string]FieldValue `json:"create,omitempty"`
}

type MutationEvidence struct {
	SourceID string `json:"source_id"`
	Number   string `json:"number"`
}

type MutationResult struct {
	Status   string                `json:"status"`
	Record   *SourceRecord         `json:"record,omitempty"`
	Current  map[string]FieldValue `json:"current,omitempty"`
	Evidence *MutationEvidence     `json:"evidence,omitempty"`
}

func ValidFieldValue(value FieldValue) bool {
	if len(value.Value) == 0 || !json.Valid(value.Value) {
		return false
	}
	if string(value.Value) == "null" {
		return true
	}
	switch value.Type {
	case FieldText, FieldRichText, FieldDate:
		var str string
		return json.Unmarshal(value.Value, &str) == nil
	case FieldNumber:
		var number int
		return json.Unmarshal(value.Value, &number) == nil
	case FieldAccounts:
		var accounts []Option
		if json.Unmarshal(value.Value, &accounts) != nil {
			return false
		}
		for _, account := range accounts {
			if account.ID == "" {
				return false
			}
		}
		return true
	case FieldLabels:
		var labels []string
		return json.Unmarshal(value.Value, &labels) == nil
	default:
		return false
	}
}

func FieldEqual(a, b FieldValue) bool {
	if a.Type != b.Type {
		return false
	}
	var av, bv any
	if json.Unmarshal(a.Value, &av) != nil || json.Unmarshal(b.Value, &bv) != nil {
		return false
	}
	aa, _ := json.Marshal(av)
	bb, _ := json.Marshal(bv)
	return string(aa) == string(bb)
}

func (p MutationParams) Valid(reconcile bool) bool {
	if p.OperationID == "" {
		return false
	}
	switch {
	case len(p.Create) > 0:
		return len(p.Patch) == 0 && p.Action == "" && len(p.ActionFields) == 0 && (reconcile || p.SourceID == "")
	case p.Action != "":
		return p.SourceID != "" && len(p.Patch) == 0
	default:
		return p.SourceID != "" && len(p.Patch) > 0 && len(p.ActionFields) == 0
	}
}
