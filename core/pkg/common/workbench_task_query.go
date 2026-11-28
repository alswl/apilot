package common

type WorkbenchTaskQuery struct {
	Keyword          string                `json:"keyword,omitempty" doc:"Case-insensitive literal substring matched against visible task search fields."`
	Expression       string                `json:"expression,omitempty" doc:"CEL expression returning bool. An empty expression matches all candidates." maxLength:"16384"`
	Fields           []WorkbenchQueryField `json:"fields,omitempty" maxItems:"100"`
	Timezone         string                `json:"timezone" doc:"IANA workspace timezone."`
	SourceIDs        []string              `json:"source_ids,omitempty" doc:"Stable scope IDs (task connection_id)."`
	StableSourceID   string                `json:"stable_source_id,omitempty" doc:"Exact connector source ID (for example, a GitHub issue ID)."`
	IssueRepository  string                `json:"issue_repository,omitempty" doc:"Repository name for an exact GitHub issue number lookup."`
	IssueNumber      string                `json:"issue_number,omitempty" doc:"Exact GitHub issue number, scoped by issue_repository."`
	States           []string              `json:"states,omitempty"`
	WatchIDs         *[]string             `json:"watch_ids,omitempty" doc:"Task IDs in the current watch set. Omit to ignore watch status. An empty array matches nothing."`
	TaskIDs          *[]string             `json:"task_ids,omitempty" doc:"Stable task IDs for sample preview. An empty array matches nothing."`
	Display          WorkbenchQueryDisplay `json:"display,omitempty"`
	SliceValue       *string               `json:"slice_value,omitempty" doc:"Group key from slices. An empty string selects missing values. Omit to include all values."`
	Limit            int                   `json:"limit,omitempty" minimum:"0" maximum:"100"`
	Offset           int                   `json:"offset,omitempty" minimum:"0"`
	RequiredRevision int64                 `json:"required_revision,omitempty" doc:"Pass the first response revision for a continuation." minimum:"0"`
	AsOfDate         string                `json:"as_of_date,omitempty" doc:"Pass the first response local YYYY-MM-DD date for a continuation."`
	PageToken        string                `json:"page_token,omitempty" doc:"Pass the first response token with the same query and display settings for a continuation."`
}

type WorkbenchQueryField struct {
	ID       string                           `json:"id"`
	Key      string                           `json:"key"`
	Name     string                           `json:"name"`
	Type     string                           `json:"type" enum:"text,single_select,multi_select,number,date"`
	Mappings map[string]WorkbenchQueryMapping `json:"mappings"`
}

type WorkbenchQueryMapping struct {
	Kind string `json:"kind" enum:"standard,native,key_value_label,label_prefix"`
	Key  string `json:"key"`
}

type WorkbenchQueryDisplay struct {
	Layout     string   `json:"layout,omitempty" enum:"list,board"`
	Fields     []string `json:"fields,omitempty"`
	GroupBy    string   `json:"group_by,omitempty"`
	ColumnBy   string   `json:"column_by,omitempty"`
	SwimlaneBy string   `json:"swimlane_by,omitempty"`
	SortBy     string   `json:"sort_by,omitempty"`
	Descending bool     `json:"descending,omitempty"`
	SumBy      string   `json:"sum_by,omitempty"`
	SliceBy    string   `json:"slice_by,omitempty"`
}

type WorkbenchQueryFieldInfo struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Display bool   `json:"display"`
	Group   bool   `json:"group"`
	Sort    bool   `json:"sort"`
	Sum     bool   `json:"sum"`
}

type WorkbenchQuerySource struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	NativeFields []WorkbenchQueryFieldInfo `json:"native_fields"`
}

type WorkbenchQueryValue struct {
	Text    *string  `json:"text,omitempty"`
	Key     string   `json:"key,omitempty"`
	Values  []string `json:"values,omitempty"`
	Keys    []string `json:"keys,omitempty"`
	Number  *float64 `json:"number,omitempty"`
	Decimal string   `json:"decimal,omitempty" doc:"Original fixed-point decimal used for exact sums."`
	Reason  string   `json:"reason,omitempty" enum:"missing,unmapped,invalid,conflict"`
}

type WorkbenchQueryItem struct {
	Task   WorkbenchTask                  `json:"task"`
	Fields map[string]WorkbenchQueryValue `json:"fields"`
	Column string                         `json:"column,omitempty"`
	Lane   string                         `json:"lane,omitempty"`
}

type WorkbenchQueryGroup struct {
	Key     string  `json:"key"`
	Name    string  `json:"name"`
	Missing bool    `json:"missing,omitempty"`
	Count   int     `json:"count"`
	Sum     *string `json:"sum,omitempty" doc:"Exact decimal sum. Omitted when there are no valid numbers."`
	Valid   int     `json:"valid"`
}

type WorkbenchQueryCell struct {
	Column string `json:"column"`
	Lane   string `json:"lane"`
	Count  int    `json:"count"`
}

type WorkbenchTaskQueryResult struct {
	Revision          int64                     `json:"revision"`
	AsOfDate          string                    `json:"as_of_date"`
	PageToken         string                    `json:"page_token"`
	QueryTotal        int                       `json:"query_total" doc:"Task count after query and before slicing."`
	SliceTotal        int                       `json:"slice_total" doc:"Task count after query and slicing, before pagination."`
	Fields            []WorkbenchQueryFieldInfo `json:"fields"`
	Sources           []WorkbenchQuerySource    `json:"sources"`
	FieldDependencies []string                  `json:"field_dependencies" doc:"Stable custom field IDs referenced by the checked CEL expression."`
	Slices            []WorkbenchQueryGroup     `json:"slices"`
	Columns           []WorkbenchQueryGroup     `json:"columns"`
	Lanes             []WorkbenchQueryGroup     `json:"lanes"`
	Cells             []WorkbenchQueryCell      `json:"cells"`
	Items             []WorkbenchQueryItem      `json:"items"`
}

// WorkbenchQueryScope is internal snapshot metadata for revalidating visibility.
type WorkbenchQueryScope struct {
	WorkbenchConnection
	AuthorizationGeneration int64
	AccountID               int64
}
