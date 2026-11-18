package workbench

import (
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/alswl/apilot/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type labelEdit struct {
	taskID, query, problem string
	labels                 []string
	pairs                  map[string]string
	pairKey, pairValue     string
}

func (m *mockup) openLabels(o sourceObject) {
	if m.editableSource(o) && !m.sourceWritePending(o.ID) {
		pairs := maps.Clone(o.LabelValues)
		if pairs == nil {
			pairs = make(map[string]string)
		}
		m.labelEditor = &labelEdit{taskID: o.ID, labels: slices.Clone(o.Labels), pairs: pairs}
	}
}

func (m *mockup) taskLabelProperty(c *ui.Context, o *sourceObject) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		ui.Row(c.Key("heading")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			components.Muted(c, tr("标签"))
			ui.Spacer(c).Grow(1)
			if m.editableSource(*o) && !m.sourceWritePending(o.ID) {
				button := components.IconButton(c.Key("edit"), "tag", tr("管理标签"), false)
				if keys := m.keyboard.display("task.labels"); keys != "" {
					button.Tooltip(tr("管理标签") + " · " + keys)
				}
				if button.Clicked() {
					m.openLabels(*o)
				}
			}
		})
		if len(o.Labels)+len(o.LabelValues) == 0 {
			components.Muted(c, tr("无标签"))
		} else {
			ui.Row(c.Key("tags")).FillWidth().Wrap().Gap(6).Children(func() {
				for _, label := range o.Labels {
					components.Tag(c.Key(label), label)
				}
				for _, key := range slices.Sorted(maps.Keys(o.LabelValues)) {
					components.KeyValueTag(c.Key("pair-"+key), key, o.LabelValues[key])
				}
			})
		}
	})
}

func (m *mockup) labelOptions(conn *connection) []string {
	if conn.SourceKind == githubIssues {
		return slices.Sorted(slices.Values(conn.IssueLabels))
	}
	unique := make(map[string]bool)
	for _, o := range m.objects {
		if o.ConnectionID == conn.ID {
			for _, label := range o.Labels {
				unique[label] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(unique))
}

func (m *mockup) labelPicker(c *ui.Context, conn *connection, label string, query *string, values *[]string, autoFocus bool) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		components.Heading(c.Key("title"), tr(label))
		if len(*values) > 0 {
			ui.Row(c.Key("picked")).FillWidth().Wrap().Gap(6).Children(func() {
				for _, value := range *values {
					ui.Row(c.Key(value)).AlignItems(ui.Center).Gap(4).Children(func() {
						components.Tag(c, value)
						if components.IconButton(c, "xmark", tr("移除标签 ")+value, false).Clicked() {
							*values = slices.DeleteFunc(*values, func(item string) bool { return item == value })
						}
					})
				}
			})
		}
		options := m.labelOptions(conn)
		search := ui.SearchField(c.Key("query"), query).Label(tr("搜索标签")).Placeholder(tr("搜索标签"))
		if autoFocus {
			search.AutoFocus()
		}
		term := strings.TrimPrefix(strings.TrimSpace(*query), "#")
		var matches []string
		for _, option := range options {
			if strings.Contains(strings.ToLower(option), strings.ToLower(term)) {
				matches = append(matches, option)
			}
		}
		submitted := search.Submitted()
		if submitted && term != "" && len(matches) > 0 {
			choice := matches[0]
			if index := slices.IndexFunc(matches, func(value string) bool { return strings.EqualFold(value, term) }); index >= 0 {
				choice = matches[index]
			}
			*values = toggleLabel(*values, choice)
			*query = ""
			c.Invalidate()
			return
		}
		if len(matches) > 0 {
			ui.Scroll(c.Key("choices")).FillWidth().MaxHeight(132).Children(func() {
				components.LabelChoices(c, matches, func(value string) bool { return slices.Contains(*values, value) }, func(value string) {
					if index := slices.Index(*values, value); index >= 0 {
						*values = slices.Delete(*values, index, index+1)
					} else {
						*values = append(*values, value)
					}
				})
			})
		}
		switch {
		case conn.SourceKind == obsidianTasks && term != "" && !containsLabelFold(options, term) && !containsLabelFold(*values, term):
			if !validObsidianLabel(term) {
				components.Muted(c, tr("标签可用字母、数字和 / _ -，不能只含数字"))
			} else if ui.Button(c.Key("add"), tr("添加标签 ")+term).Clicked() || submitted {
				*values = append(*values, term)
				*query = ""
			}
		case len(matches) == 0:
			components.Muted(c, tr("无匹配标签"))
		}
	})
}

func validObsidianLabel(value string) bool {
	if value == "" {
		return false
	}
	hasNonNumber := false
	for _, segment := range strings.Split(value, "/") {
		if segment == "" {
			return false
		}
		for _, char := range segment {
			switch {
			case unicode.IsLetter(char), unicode.IsMark(char), char == '_', char == '-':
				hasNonNumber = true
			case unicode.IsNumber(char):
			default:
				return false
			}
		}
	}
	return hasNonNumber
}

func (m *mockup) labelsDialog(c *ui.Context) {
	open := m.labelEditor != nil
	ui.Modal(c, &open, func() {
		edit := m.labelEditor
		index := slices.IndexFunc(m.objects, func(o sourceObject) bool { return o.ID == edit.taskID })
		if index < 0 {
			open = false
			return
		}
		object := m.objects[index]
		conn := m.taskConnection(object)
		if conn == nil {
			open = false
			return
		}
		ui.Column(c).Width(460).Gap(12).Children(func() {
			components.Title(c, tr("管理标签"))
			components.Muted(c, tr(object.Title)+" · "+tr(conn.Name))
			ui.Scroll(c.Key("body")).FillWidth().MaxHeight(340).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					m.labelPicker(c.Key("picker"), conn, "标签", &edit.query, &edit.labels, true)
					ui.Divider(c)
					m.labelPairs(c.Key("pairs"), edit)
				})
			})
			if edit.problem != "" {
				components.Muted(c, edit.problem)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c.Key("cancel"), tr("取消")).Clicked() {
					open = false
				}
				if ui.PrimaryButton(c.Key("save"), tr("保存标签")).Disabled(sameLabelSet(object.Labels, edit.labels) && maps.Equal(object.LabelValues, edit.pairs)).Clicked() {
					if !sameLabelSet(object.Labels, edit.labels) {
						draft := sourceDraft(object, conn.SourceKind)
						draft.labels = slices.Clone(edit.labels)
						edit.problem = m.submitSourceEdit(draft, m.after)
					}
					if edit.problem == "" {
						m.objects[index].LabelValues = maps.Clone(edit.pairs)
					}
					open = edit.problem != ""
				}
			})
		})
	})
	if !open {
		m.labelEditor = nil
	}
}

func toggleLabel(values []string, label string) []string {
	if i := slices.Index(values, label); i >= 0 {
		return slices.Delete(values, i, i+1)
	}
	return append(values, label)
}

func containsLabelFold(values []string, label string) bool {
	return slices.ContainsFunc(values, func(value string) bool { return strings.EqualFold(value, label) })
}

func sameLabelSet(a, b []string) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(label string) bool { return !slices.Contains(b, label) })
}

func (m *mockup) labelPairs(c *ui.Context, edit *labelEdit) {
	components.Heading(c.Key("heading"), tr("本地键值标签"))
	for _, key := range slices.Sorted(maps.Keys(edit.pairs)) {
		ui.Row(c.Key(key)).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			components.KeyValueTag(c, key, edit.pairs[key])
			ui.Spacer(c).Grow(1)
			if components.IconButton(c.Key("edit"), "pencil", tr("编辑键值标签 ")+key, false).Clicked() {
				edit.pairKey, edit.pairValue = key, edit.pairs[key]
			}
			if components.IconButton(c.Key("remove"), "xmark", tr("移除标签 ")+key, false).Clicked() {
				delete(edit.pairs, key)
			}
		})
	}
	ui.Row(c.Key("input")).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.TextInput(c.Key("key"), &edit.pairKey).Label(tr("标签键")).Placeholder(tr("键")).Grow(1).MinWidth(0)
		value := ui.TextInput(c.Key("value"), &edit.pairValue).Label(tr("标签值")).Placeholder(tr("值")).Grow(1).MinWidth(0)
		key, val := strings.TrimSpace(edit.pairKey), strings.TrimSpace(edit.pairValue)
		valid := key != "" && val != "" && !strings.ContainsAny(key, ":=")
		if (components.IconButton(c.Key("add"), "plus", tr("添加键值标签"), !valid).Clicked() || value.Submitted()) && valid {
			edit.pairs[key] = val
			edit.pairKey, edit.pairValue = "", ""
		}
	})
}

func taskLabelValues(o sourceObject) []string {
	labels := slices.Clone(o.Labels)
	for _, key := range slices.Sorted(maps.Keys(o.LabelValues)) {
		labels = append(labels, key+":"+o.LabelValues[key])
	}
	return labels
}
