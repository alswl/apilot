package services

import (
	"encoding/json"
	"maps"
	"math/big"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
	"golang.org/x/text/cases"
)

var (
	queryFieldKey = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	queryDecimal  = regexp.MustCompile(`^[+-]?[0-9]{1,12}(\.[0-9]{1,6})?$`)
)

var workbenchQueryStandard = []common.WorkbenchQueryFieldInfo{
	queryFieldInfo("task.", "id", "Task ID", "text"),
	queryFieldInfo("task.", "title", "Title", "text"),
	queryFieldInfo("task.", "source_id", "Source", "source"),
	queryFieldInfo("task.", "status", "Status", "single_select"),
	queryFieldInfo("task.", "labels", "Labels", "multi_select"),
	{ID: "task.assignees", Key: "assignees", Name: "Assignees", Type: "multi_select", Display: true, Sort: true},
	queryFieldInfo("task.", "priority", "Priority", "integer"),
	queryFieldInfo("task.", "due", "Due date", "date"),
	queryFieldInfo("task.", "updated", "Last updated", "timestamp"),
}

func queryFieldInfo(prefix, key, name, kind string) common.WorkbenchQueryFieldInfo {
	return common.WorkbenchQueryFieldInfo{
		ID: prefix + key, Key: key, Name: name, Type: kind, Display: true,
		Group: kind == "single_select" || kind == "source",
		Sort:  kind != "multi_select", Sum: kind == "number" || kind == "integer",
	}
}

func workbenchQueryFields(fields []common.WorkbenchQueryField) (map[string]common.WorkbenchQueryFieldInfo, error) {
	directory := make(map[string]common.WorkbenchQueryFieldInfo, len(fields)+len(workbenchQueryStandard))
	for _, field := range workbenchQueryStandard {
		directory[field.ID] = field
	}
	if len(fields) > 100 {
		return nil, common.Err("INVALID_ARGUMENT", "At most 100 custom fields are supported")
	}
	keys := make(map[string]bool)
	for _, field := range fields {
		if field.ID == "" || strings.TrimSpace(field.ID) != field.ID || strings.HasPrefix(field.ID, "task.") ||
			strings.HasPrefix(field.ID, "native.") || strings.TrimSpace(field.Name) == "" ||
			!queryFieldKey.MatchString(field.Key) || queryReservedKey(field.Key) || keys[field.Key] || directory[field.ID].ID != "" {
			return nil, common.Err("INVALID_ARGUMENT", "Invalid or duplicate custom field: "+field.Key)
		}
		keys[field.Key] = true
		switch field.Type {
		case "text", "single_select", "multi_select", "number", "date":
		default:
			return nil, common.Err("INVALID_ARGUMENT", "Invalid custom field type: "+field.Type)
		}
		for scope, rule := range field.Mappings {
			if strings.TrimSpace(scope) != scope || scope == "" || strings.TrimSpace(rule.Key) == "" {
				return nil, common.Err("INVALID_ARGUMENT", "Invalid field mapping: "+field.Key)
			}
			switch rule.Kind {
			case "standard":
				if directory["task."+rule.Key].ID == "" {
					return nil, common.Err("INVALID_ARGUMENT", "Unknown standard field: "+rule.Key)
				}
			case "native", "key_value_label", "label_prefix":
			default:
				return nil, common.Err("INVALID_ARGUMENT", "Unknown field mapping kind: "+rule.Kind)
			}
		}
		info := queryFieldInfo("", field.Key, strings.TrimSpace(field.Name), field.Type)
		info.ID = field.ID
		directory[field.ID] = info
	}
	return directory, nil
}

func queryReservedKey(key string) bool {
	switch key {
	case "as", "break", "const", "continue", "else", "false", "for", "function", "if", "import", "in",
		"let", "loop", "package", "namespace", "null", "return", "true", "var", "void", "while":
		return true
	}
	return false
}

type queryTask struct {
	item   common.WorkbenchQueryItem
	native map[string]string
}

type queryNativeValue struct {
	kind, reason string
	values       []string
}

func workbenchQueryNative(task common.WorkbenchTask, connector WorkbenchConnector) map[string]queryNativeValue {
	native := make(map[string]queryNativeValue)
	for _, key := range connector.SearchFields {
		value := task.NativeFields[key]
		switch key {
		case "repo":
			if repo := task.NativeFields["repository"]; repo != "" {
				value = repo
			}
		case "file":
			if file := task.NativeFields["path"]; file != "" {
				value = path.Base(file)
			}
		case "state":
			if value == "" {
				value = task.NativeFields["status"]
			}
		}
		native[key] = queryNativeValue{kind: workbenchSearchFieldType(key), values: []string{value}}
	}
	for key, raw := range task.NativeFields {
		if id, ok := strings.CutPrefix(key, "native."); ok && id != "" {
			native[id] = decodeQueryNative(raw)
		}
	}
	return native
}

func workbenchSearchFieldType(key string) string {
	switch key {
	case "created", "start", "scheduled", "done", "cancelled":
		return "date"
	case "line":
		return "number"
	default:
		return "text"
	}
}

func decodeQueryNative(raw string) queryNativeValue {
	var field protocol.FieldValue
	if json.Unmarshal([]byte(raw), &field) != nil || !json.Valid(field.Value) {
		return queryNativeValue{kind: "text", reason: "invalid"}
	}
	value := queryNativeValue{kind: "text"}
	switch field.Type {
	case protocol.FieldNumber:
		value.kind = "number"
	case protocol.FieldDate:
		value.kind = "date"
	case protocol.FieldLabels, protocol.FieldAccounts:
		value.kind = "multi_select"
	case protocol.FieldText, protocol.FieldRichText:
	default:
		value.reason = "invalid"
	}
	if string(field.Value) == "null" || value.reason != "" {
		return value
	}
	switch field.Type {
	case protocol.FieldNumber:
		if queryDecimal.Match(field.Value) {
			value.values = []string{string(field.Value)}
		} else {
			value.reason = "invalid"
		}
	case protocol.FieldLabels:
		if json.Unmarshal(field.Value, &value.values) != nil {
			value.reason = "invalid"
		}
	case protocol.FieldAccounts:
		var options []protocol.Option
		if json.Unmarshal(field.Value, &options) != nil {
			value.reason = "invalid"
		}
		for _, option := range options {
			if option.ID == "" {
				value.reason = "invalid"
				break
			}
			name := option.Label
			if name == "" {
				name = option.ID
			}
			value.values = append(value.values, name)
		}
	default:
		var text string
		if json.Unmarshal(field.Value, &text) != nil {
			value.reason = "invalid"
		}
		value.values = []string{text}
	}
	return value
}

func projectWorkbenchQueryTask(task common.WorkbenchTask, source common.WorkbenchConnection, native map[string]queryNativeValue, fields []common.WorkbenchQueryField, zone *time.Location) queryTask {
	projected := make(map[string]common.WorkbenchQueryValue, len(fields)+len(workbenchQueryStandard)+len(native))
	labels := slices.Clone(task.Labels)
	if task.Kind == common.WorkbenchObsidianTasks {
		for i, label := range labels {
			labels[i] = strings.TrimPrefix(label, "#")
		}
	}
	for _, label := range task.WorkbenchLabels {
		labels = append(labels, label.Name)
	}
	for _, key := range slices.Sorted(maps.Keys(task.LabelValues)) {
		labels = append(labels, key+":"+task.LabelValues[key])
	}
	status := task.State
	if !validWorkbenchState(status) {
		status = "unknown"
	}
	projected["task.id"] = queryText(task.ID)
	projected["task.title"] = queryText(task.Title)
	projected["task.source_id"] = common.WorkbenchQueryValue{Text: &source.Name, Key: task.ConnectionID}
	projected["task.status"] = common.WorkbenchQueryValue{Text: &status, Key: status}
	projected["task.labels"] = queryExactMultiSelect(labels)
	projected["task.assignees"] = queryRawList(task.Assignees)
	projected["task.priority"] = common.WorkbenchQueryValue{Reason: "missing"}
	if task.Priority != nil {
		value := float64(*task.Priority)
		projected["task.priority"] = common.WorkbenchQueryValue{Number: &value, Decimal: strconv.Itoa(*task.Priority)}
	}
	projected["task.due"] = queryConvert("date", []string{task.DueDate}, zone)
	projected["task.updated"] = queryUpdated(task)

	nativeInputs := make(map[string]string, len(native))
	for key, raw := range native {
		value := queryConvert(raw.kind, raw.values, zone)
		if raw.reason != "" {
			value = common.WorkbenchQueryValue{Reason: raw.reason}
		}
		projected["native."+key] = value
		if value.Reason == "" {
			nativeInputs[key] = strings.Join(raw.values, ", ")
		}
	}
	for _, field := range fields {
		projected[field.ID] = queryMappedValue(task, field, projected, native, zone)
	}
	return queryTask{item: common.WorkbenchQueryItem{Task: task, Fields: projected}, native: nativeInputs}
}

func queryExactMultiSelect(raw []string) common.WorkbenchQueryValue {
	labels := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			labels = append(labels, value)
		}
	}
	if len(labels) == 0 {
		return common.WorkbenchQueryValue{Reason: "missing"}
	}
	return common.WorkbenchQueryValue{Values: labels, Keys: slices.Clone(labels)}
}

func queryUpdated(task common.WorkbenchTask) common.WorkbenchQueryValue {
	var updated time.Time
	if len(task.Activity) != 0 {
		updated = task.Activity[len(task.Activity)-1].Time
	}
	if updated.IsZero() {
		updated, _ = time.Parse(time.RFC3339Nano, task.NativeFields["updated_at"])
	}
	if updated.IsZero() {
		return common.WorkbenchQueryValue{Reason: "missing"}
	}
	return queryText(updated.UTC().Format("2006-01-02T15:04:05.000000000Z"))
}

func queryMappedValue(task common.WorkbenchTask, field common.WorkbenchQueryField, projected map[string]common.WorkbenchQueryValue, native map[string]queryNativeValue, zone *time.Location) common.WorkbenchQueryValue {
	rule, exists := field.Mappings[task.ConnectionID]
	if !exists {
		return common.WorkbenchQueryValue{Reason: "unmapped"}
	}
	var raw []string
	switch rule.Kind {
	case "standard":
		value := projected["task."+rule.Key]
		if value.Reason != "" {
			return common.WorkbenchQueryValue{Reason: value.Reason}
		}
		switch {
		case rule.Key == "source_id":
			raw = []string{value.Key}
		case value.Text != nil:
			raw = []string{*value.Text}
		case value.Number != nil:
			raw = []string{value.Decimal}
		default:
			raw = value.Values
		}
	case "native":
		value, ok := native[rule.Key]
		if !ok {
			return common.WorkbenchQueryValue{Reason: "unmapped"}
		}
		if value.reason != "" {
			return common.WorkbenchQueryValue{Reason: value.reason}
		}
		raw = value.values
	case "key_value_label":
		raw = []string{task.LabelValues[rule.Key]}
	case "label_prefix":
		for _, label := range task.Labels {
			if suffix, ok := strings.CutPrefix(label, rule.Key); ok {
				raw = append(raw, suffix)
			}
		}
	}
	return queryConvert(field.Type, raw, zone)
}

func queryText(value string) common.WorkbenchQueryValue {
	value = strings.TrimSpace(value)
	if value == "" {
		return common.WorkbenchQueryValue{Reason: "missing"}
	}
	return common.WorkbenchQueryValue{Text: &value}
}

func queryRawList(values []string) common.WorkbenchQueryValue {
	if len(values) == 0 {
		return common.WorkbenchQueryValue{Reason: "missing"}
	}
	return common.WorkbenchQueryValue{Values: values}
}

func queryConvert(kind string, raw []string, zone *time.Location) common.WorkbenchQueryValue {
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return common.WorkbenchQueryValue{Reason: "missing"}
	}
	switch kind {
	case "single_select", "multi_select":
		seen := make(map[string]bool)
		var labels, keys []string
		fold := cases.Fold()
		for _, value := range values {
			key := fold.String(value)
			if !seen[key] {
				seen[key] = true
				labels, keys = append(labels, value), append(keys, key)
			}
		}
		if kind == "multi_select" {
			return common.WorkbenchQueryValue{Values: labels, Keys: keys}
		}
		if len(keys) == 1 {
			return common.WorkbenchQueryValue{Text: &labels[0], Key: keys[0]}
		}
		return common.WorkbenchQueryValue{Reason: "conflict"}
	}
	if len(slices.Compact(values)) != 1 {
		return common.WorkbenchQueryValue{Reason: "invalid"}
	}
	text := values[0]
	switch kind {
	case "number":
		if queryDecimal.MatchString(text) {
			number, _ := strconv.ParseFloat(text, 64)
			return common.WorkbenchQueryValue{Number: &number, Decimal: text}
		}
	case "date":
		if date, err := time.Parse(time.DateOnly, text); err == nil && date.Year() > 0 {
			return queryText(text)
		}
		if date, err := time.Parse(time.RFC3339Nano, text); err == nil {
			local := date.In(zone)
			if local.Year() > 0 && local.Year() <= 9999 {
				return queryText(local.Format(time.DateOnly))
			}
		}
	default:
		return queryText(text)
	}
	return common.WorkbenchQueryValue{Reason: "invalid"}
}

func queryAddDecimal(sum *big.Int, value string) {
	whole, decimal, _ := strings.Cut(value, ".")
	fixed, _ := new(big.Int).SetString(whole+decimal+strings.Repeat("0", 6-len(decimal)), 10)
	sum.Add(sum, fixed)
}

func queryFormatDecimal(sum *big.Int) string {
	abs := new(big.Int).Abs(sum).String()
	if len(abs) < 7 {
		abs = strings.Repeat("0", 7-len(abs)) + abs
	}
	whole, fraction := abs[:len(abs)-6], strings.TrimRight(abs[len(abs)-6:], "0")
	if fraction != "" {
		whole += "." + fraction
	}
	if sum.Sign() < 0 {
		whole = "-" + whole
	}
	return whole
}
