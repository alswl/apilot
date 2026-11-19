package workbench

import (
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type searchTerm struct {
	field, value    string
	exact, excluded bool
}

// Search groups are ORed. Each group contains terms that must all match.
type taskQuery [][]searchTerm

func parseTaskQuery(text string, fields map[string]bool) (taskQuery, string) {
	var groups taskQuery
	var terms []searchTerm
	var token strings.Builder
	quoted, escaped := false, false
	separator := -1
	flush := func() string {
		raw := token.String()
		token.Reset()
		at := separator
		separator = -1
		if raw == "" {
			return ""
		}
		term := searchTerm{value: raw}
		if raw[0] == '-' && len(raw) > 1 {
			term.excluded, raw = true, raw[1:]
			at--
		}
		term.value = raw
		if at >= 0 {
			term.field, term.value = strings.ToLower(raw[:at]), raw[at+1:]
			if !fields[term.field] {
				return tr("未知搜索字段：") + term.field
			}
			if strings.HasPrefix(term.value, "=") {
				term.exact = true
				term.value = term.value[1:]
			}
			if term.field == "has" && !fields[strings.ToLower(term.value)] {
				return tr("未知搜索字段：") + term.value
			}
		} else if strings.HasPrefix(raw, "#") {
			term.field, term.value = "label", raw[1:]
		}
		if term.value == "" {
			return tr("请输入搜索字段的值")
		}
		terms = append(terms, term)
		return ""
	}
	for _, char := range text {
		switch {
		case escaped:
			token.WriteRune(char)
			escaped = false
		case quoted && char == '\\':
			escaped = true
		case char == '"':
			quoted = !quoted
		case !quoted && (unicode.IsSpace(char) || char == '|'):
			if problem := flush(); problem != "" {
				return nil, problem
			}
			if char == '|' {
				if len(terms) == 0 {
					return nil, tr("请在 | 两侧填写条件")
				}
				groups, terms = append(groups, terms), nil
			}
		case !quoted && char == ':' && separator < 0:
			separator = token.Len()
			token.WriteRune(char)
		default:
			token.WriteRune(char)
		}
	}
	if quoted {
		return nil, tr("搜索条件缺少结束引号")
	}
	if problem := flush(); problem != "" {
		return nil, problem
	}
	if len(terms) > 0 {
		groups = append(groups, terms)
	} else if len(groups) > 0 {
		return nil, tr("请在 | 两侧填写条件")
	}
	return groups, ""
}

var commonSearchFields = []string{"source", "label", "status", "priority", "due", "id", "title", "has"}

func (m *mockup) parseSearch() (taskQuery, string) {
	fields := make(map[string]bool)
	for _, field := range commonSearchFields {
		fields[field] = true
	}
	for _, conn := range m.connectionsData {
		for _, field := range conn.SearchFields {
			fields[field] = true
		}
	}
	return parseTaskQuery(m.search, fields)
}

func (m *mockup) searchValues(object sourceObject, field string) []string {
	switch field {
	case "source":
		for _, conn := range m.connectionsData {
			if conn.ID == object.ConnectionID {
				return append([]string{conn.ID, conn.Name, conn.ConnectorID}, conn.SearchAliases...)
			}
		}
	case "label":
		return taskLabelValues(object)
	case "id":
		return []string{object.ID, object.NativeID, displayTaskID(object)}
	case "file":
		if value := object.NativeFields["path"]; value != "" {
			return []string{path.Base(value)}
		}
	case "depends_on":
		if value := object.NativeFields["depends_on"]; value != "" {
			return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		}
	case "assignee":
		return object.Assignees
	case "title":
		return []string{object.Title}
	case "status":
		return []string{object.NativeFields[field], m.taskStateLabel(object)}
	case "priority", "due":
		if value := object.NativeFields[field]; value != "" {
			return []string{value}
		}
	case "":
		values := []string{object.Title, object.NativeID, object.Locator, m.connectionName(object.ConnectionID)}
		values = append(values, taskLabelValues(object)...)
		values = append(values, object.Assignees...)
		for _, value := range object.NativeFields {
			values = append(values, value)
		}
		return values
	default:
		for _, conn := range m.connectionsData {
			if conn.ID == object.ConnectionID && slices.Contains(conn.SearchFields, field) {
				if value := object.NativeFields[field]; value != "" {
					return []string{value}
				}
			}
		}
	}
	return nil
}

func (m *mockup) matchesTask(object sourceObject, query taskQuery) bool {
	if len(query) == 0 {
		return true
	}
	for _, group := range query {
		if !slices.ContainsFunc(group, func(term searchTerm) bool {
			var found bool
			if term.field == "has" {
				found = len(m.searchValues(object, strings.ToLower(term.value))) > 0
			} else {
				wanted := strings.ToLower(term.value)
				for _, value := range m.searchValues(object, term.field) {
					for _, candidate := range []string{strings.ToLower(value), strings.ToLower(tr(value))} {
						if candidate == wanted || (!term.exact && strings.Contains(candidate, wanted)) {
							found = true
						}
					}
				}
			}
			return found == term.excluded
		}) {
			return true
		}
	}
	return false
}

func (m *mockup) searchHelp(c *ui.Context) {
	components.Muted(c, tr("空格并且 · | 或 · - 排除 · 双引号包住短语"))
	m.searchFieldButtons(c.Key("common"), commonSearchFields)
	labels := make(map[string]bool)
	for _, object := range m.objects {
		if m.sourceFilter == "全部来源" || m.sourceFilter == m.connectionName(object.ConnectionID) {
			for _, label := range taskLabelValues(object) {
				labels[label] = true
			}
		}
	}
	if len(labels) > 0 {
		ui.Row(c.Key("labels")).FillWidth().AlignItems(ui.Center).Wrap().Gap(8).Children(func() {
			components.Muted(c, tr("标签"))
			for _, label := range slices.Sorted(maps.Keys(labels)) {
				if ui.Button(c.Key(label), "#"+label).Clicked() {
					m.appendSearch(c, "label:="+strconv.Quote(label))
				}
			}
		})
	}
	for _, conn := range m.connectionsData {
		if m.sourceFilter != "全部来源" && m.sourceFilter != conn.Name {
			continue
		}
		if len(conn.SearchFields) > 0 {
			components.Heading(c.Key(conn.ID), tr(conn.Name))
			m.searchFieldButtons(c.Key(conn.ID+"-fields"), conn.SearchFields)
		}
	}
}

func (m *mockup) searchFieldButtons(c *ui.Context, fields []string) {
	ui.Row(c).FillWidth().Wrap().Gap(8).Children(func() {
		for _, field := range fields {
			if ui.Button(c.Key(field), field+":").Clicked() {
				m.appendSearch(c, field+":")
			}
		}
	})
}

func (m *mockup) appendSearch(c *ui.Context, term string) {
	m.search = strings.TrimSpace(m.search + " " + term)
	if m.compactTaskList() {
		m.searchExpanded = false
	}
	m.keyboard.focusSearch = true
	c.Invalidate()
}

// Source filters add to the same query as the text field, so they can be saved
// and combined with labels without maintaining a second filter model.
func (m *mockup) sourceQuickFilters(c *ui.Context) {
	for _, conn := range m.connectionsData {
		if conn.Name != m.sourceFilter || len(conn.SearchFields) == 0 {
			continue
		}
		ui.Row(c.Key("quick-filters-" + conn.ID)).FillWidth().Wrap().Gap(8).Children(func() {
			fields := conn.SearchFields[:min(2, len(conn.SearchFields))]
			if conn.SourceKind == "obsidian_tasks" {
				fields = []string{"file", "priority", "due", "scheduled", "recurrence", "on_completion"}
			}
			for _, field := range fields {
				values := make(map[string]bool)
				for _, object := range m.objects {
					if object.ConnectionID == conn.ID {
						for _, value := range m.searchValues(object, field) {
							values[value] = true
						}
					}
				}
				if len(values) == 0 {
					continue
				}
				title := tr(searchFieldTitle(field))
				options := append([]string{title}, slices.Sorted(maps.Keys(values))...)
				selected := title
				if ui.Select(c.Key(field), &selected, options).Width(150).Changed() && selected != title {
					m.appendSearch(c, field+":="+strconv.Quote(selected))
				}
			}
		})
		return
	}
}

func searchFieldTitle(field string) string {
	if title := map[string]string{"file": "文件名", "line": "行号", "created": "创建日期", "cancelled": "取消日期", "recurrence": "重复规则", "depends_on": "依赖任务", "on_completion": "完成后操作", "priority": "优先级", "due": "截止日", "path": "路径", "section": "章节", "repo": "仓库", "assignee": "负责人", "milestone": "里程碑", "state_reason": "关闭原因", "start": "开始日期", "scheduled": "计划日期", "done": "完成日期"}[field]; title != "" {
		return title
	}
	return field
}
