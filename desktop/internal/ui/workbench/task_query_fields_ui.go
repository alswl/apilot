package workbench

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alswl/apilot/core/pkg/common"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/text/cases"
)

func foldQueryOption(raw string) string { return cases.Fold().String(strings.TrimSpace(raw)) }

func (m *mockup) queryFieldsDialog(c *ui.Context) {
	q := &m.customQuery
	ui.Modal(c, &q.fieldsOpen, func() {
		ui.Column(c).Width(460).Gap(10).Children(func() {
			components.Title(c, tr("管理字段"))
			if q.field == nil {
				for _, field := range q.config.Fields {
					if ui.Button(c.Key(field.ID), field.Name+" · "+field.Key).Clicked() {
						draft := field
						draft.Mappings = maps.Clone(field.Mappings)
						q.field, q.fieldID, q.fieldPreview, q.fieldProblem = &draft, field.ID, nil, ""
					}
				}
				if ui.PrimaryButton(c.Key("new"), tr("新字段")).Clicked() {
					q.field = &common.WorkbenchQueryField{ID: common.ID(), Type: "text", Mappings: map[string]common.WorkbenchQueryMapping{}}
					q.fieldID, q.fieldPreview, q.fieldProblem = "", nil, ""
				}
				if ui.Button(c.Key("done"), tr("完成")).Clicked() {
					q.fieldsOpen = false
				}
			} else if q.fieldDelete {
				m.queryFieldDeleteConfirmation(c.Key("delete-confirmation"), *q.field)
			} else {
				m.queryFieldEditor(c.Key("editor"), q.field)
			}
			if q.syncWarning != "" {
				components.Muted(c.Key("sync-warning"), q.syncWarning)
			}
			if q.fieldProblem != "" {
				components.Muted(c.Key("problem"), q.fieldProblem)
			}
		})
	})
	if !q.fieldsOpen {
		q.fieldDelete = false
	}
}

func (m *mockup) queryFieldEditor(c *ui.Context, draft *common.WorkbenchQueryField) {
	q := &m.customQuery
	components.TextField(c.Key("name"), tr("显示名称"), &draft.Name)
	components.TextField(c.Key("key"), tr("查询键"), &draft.Key).Disabled(q.fieldID != "")
	kinds := []string{"text", "single_select", "multi_select", "number", "date"}
	queryMappingSelect(c.Key("type"), &draft.Type, kinds, []string{tr("文本"), tr("单选"), tr("多选"), tr("数字"), tr("日期")}, tr("字段类型"), q.fieldID != "")
	if q.result != nil {
		ui.Scroll(c.Key("sources")).MaxHeight(230).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				for _, source := range q.result.Sources {
					m.queryFieldMapping(c.Key(source.ID), draft, source)
				}
			})
		})
	}
	if preview := q.fieldPreview; preview != nil {
		for _, sample := range preview.Items {
			value := sample.Fields[draft.ID]
			message := queryValueText(value)
			if value.Reason != "" {
				message = tr(map[string]string{"missing": "缺值", "unmapped": "未映射", "invalid": "无效值", "conflict": "多个不同值"}[value.Reason])
			}
			components.Muted(c.Key(sample.Task.ID), sample.Task.Title+" · "+message)
		}
	}
	if q.problem != "" {
		components.Muted(c.Key("store-problem"), q.problem)
	}
	ui.Row(c.Key("actions")).Wrap().Gap(8).Children(func() {
		if ui.Button(c.Key("back"), tr("返回")).Clicked() {
			q.field, q.fieldPreview, q.fieldProblem = nil, nil, ""
		}
		if ui.Button(c.Key("preview"), tr("预览")).Clicked() {
			m.previewQueryField(*draft, false)
		}
		if q.fieldID != "" && ui.Button(c.Key("delete"), tr("删除")).Clicked() {
			q.fieldDelete = true
		}
		if ui.PrimaryButton(c.Key("save"), tr("保存")).Disabled(strings.TrimSpace(draft.Name) == "" || strings.TrimSpace(draft.Key) == "").Clicked() {
			m.previewQueryField(*draft, true)
		}
	})
}

func (m *mockup) queryFieldDeleteConfirmation(c *ui.Context, field common.WorkbenchQueryField) {
	q := &m.customQuery
	references := taskQueryFieldReferences(q.config, field.ID)
	if index := slices.IndexFunc(q.config.Fields, func(candidate common.WorkbenchQueryField) bool { return candidate.ID == field.ID }); index >= 0 {
		field = q.config.Fields[index]
	}
	components.Heading(c.Key("title"), fmt.Sprintf(tr("删除字段「%s」？"), field.Name))
	if len(references) > 0 {
		components.Muted(c.Key("notice"), tr("字段仍被视图使用"))
		ui.Scroll(c.Key("references")).FillWidth().MaxHeight(180).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for _, view := range references {
					components.Body(c.Key(view.ID), view.Name)
				}
			})
		})
	} else {
		components.Muted(c.Key("notice"), tr("没有已保存的视图引用此字段"))
	}
	ui.Row(c.Key("actions")).Gap(8).Justify(ui.End).Children(func() {
		if ui.Button(c.Key("cancel"), tr("取消")).AutoFocus().Clicked() {
			q.fieldDelete = false
		}
		if ui.Button(c.Key("confirm"), tr("确认删除")).Disabled(len(references) > 0).Clicked() {
			err := m.updateTaskQueryConfig(func(cfg *taskQueryConfig) error {
				cfg.Fields = slices.DeleteFunc(cfg.Fields, func(candidate common.WorkbenchQueryField) bool { return candidate.ID == field.ID })
				return nil
			})
			m.setTaskQueryProblem(err)
			if err == nil {
				q.field, q.fieldDelete = nil, false
				m.invalidateTaskQuery()
			}
		}
	})
	if q.problem != "" {
		components.Muted(c.Key("store-problem"), q.problem)
	}
}

func queryMappingSelect(c *ui.Context, value *string, ids, labels []string, label string, disabled bool) bool {
	selected := labels[0]
	if i := slices.Index(ids, *value); i >= 0 {
		selected = labels[i]
	}
	var changed bool
	ui.Field(c, label, func() {
		changed = ui.Select(c.Key("choose"), &selected, labels).Label(label).Disabled(disabled).FillWidth().Changed()
	})
	if changed {
		if i := slices.Index(labels, selected); i >= 0 && *value != ids[i] {
			*value = ids[i]
			return true
		}
	}
	return false
}

func (m *mockup) queryFieldMapping(c *ui.Context, field *common.WorkbenchQueryField, source common.WorkbenchQuerySource) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		components.Heading(c, source.Name)
		mapping := field.Mappings[source.ID]
		if queryMappingSelect(c.Key("kind"), &mapping.Kind,
			[]string{"", "standard", "native", "key_value_label", "label_prefix"},
			[]string{tr("未映射"), tr("标准字段"), tr("来源字段"), tr("键值标签"), tr("标签前缀")}, tr("来源映射"), false) {
			mapping.Key = ""
		}
		switch mapping.Kind {
		case "standard", "native":
			fields := source.NativeFields
			if mapping.Kind == "standard" {
				fields = slices.DeleteFunc(slices.Clone(m.customQuery.result.Fields), func(field common.WorkbenchQueryFieldInfo) bool { return !strings.HasPrefix(field.ID, "task.") })
			}
			selected := mapping.Key
			if len(fields) == 0 {
				components.Muted(c, tr("快照未公开来源字段"))
			} else {
				options := slices.Clone(fields)
				for i := range options {
					options[i].ID = options[i].Key
				}
				if queryFieldSelect(c.Key("field"), &selected, options, tr("字段"), tr("选择字段"), func(common.WorkbenchQueryFieldInfo) bool { return true }) {
					mapping.Key = selected
				}
			}
		case "label_prefix", "key_value_label":
			ui.TextInput(c.Key("key"), &mapping.Key).Label(tr("映射键")).FillWidth()
		}
		if field.Mappings == nil {
			field.Mappings = make(map[string]common.WorkbenchQueryMapping)
		}
		if mapping.Kind == "" {
			delete(field.Mappings, source.ID)
		} else {
			field.Mappings[source.ID] = mapping
		}
	})
}

func (m *mockup) previewQueryField(draft common.WorkbenchQueryField, commit bool) {
	q := &m.customQuery
	if m.live == nil || m.win == nil || m.liveStale || m.liveDisconnected {
		return
	}
	if q.previewCancel != nil {
		q.previewCancel()
	}
	draft.Mappings = maps.Clone(draft.Mappings)
	draft.Name, draft.Key = strings.TrimSpace(draft.Name), strings.TrimSpace(draft.Key)
	fields := slices.Clone(q.config.Fields)
	index := slices.IndexFunc(fields, func(f common.WorkbenchQueryField) bool { return f.ID == draft.ID })
	if index < 0 {
		fields = append(fields, draft)
	} else {
		fields[index] = draft
	}
	q.previewSequence++
	seq, win, remote := q.previewSequence, m.win, m.live
	ctx, cancel := context.WithCancel(context.Background())
	q.previewCancel = cancel
	q.fieldProblem = ""
	input := common.WorkbenchTaskQuery{Timezone: q.config.Timezone, Fields: fields, Limit: 3}
	sources := []common.WorkbenchQuerySource{}
	if q.result != nil {
		sources = slices.Clone(q.result.Sources)
	}
	go func() {
		result, err := remote.QueryWorkbenchTasks(ctx, input)
		if err == nil && !commit && len(sources) > 0 {
			result.Items = nil
			for _, source := range sources {
				sample := input
				sample.SourceIDs, sample.Limit = []string{source.ID}, 1
				preview, readErr := remote.QueryWorkbenchTasks(ctx, sample)
				if readErr != nil || preview.Revision != result.Revision || preview.AsOfDate != result.AsOfDate {
					err = common.Err("STALE_REVISION", "Task snapshot changed")
					break
				}
				result.Items = append(result.Items, preview.Items...)
			}
		}
		mygo.RunOnMain(func() {
			if ctx.Err() != nil || win.IsDestroyed() || q.previewSequence != seq || q.field == nil || q.field.ID != draft.ID || !q.fieldsOpen {
				return
			}
			if err != nil {
				q.fieldProblem = err.Error()
			} else if commit {
				err = m.updateTaskQueryConfig(func(cfg *taskQueryConfig) error {
					cfg.Fields = fields
					return nil
				})
				m.setTaskQueryProblem(err)
				if err == nil {
					q.field = nil
					m.invalidateTaskQuery()
				}
			} else {
				q.fieldPreview = &result
			}
			win.Invalidate()
		})
	}()
}

func queryValueText(value common.WorkbenchQueryValue) string {
	switch {
	case value.Text != nil:
		return *value.Text
	case value.Number != nil:
		return value.Decimal
	case len(value.Values) > 0:
		return strings.Join(value.Values, ", ")
	default:
		return "—"
	}
}

func (m *mockup) customQueryProperties(c *ui.Context, taskID string) {
	q := &m.customQuery
	if q.result == nil || len(q.config.Fields) == 0 {
		return
	}
	for _, item := range q.result.Items {
		if item.Task.ID != taskID {
			continue
		}
		properties := make([]components.Property, 0, len(q.config.Fields))
		for _, field := range q.config.Fields {
			value := item.Fields[field.ID]
			text := queryValueText(value)
			if value.Reason != "" {
				text += " · " + tr(map[string]string{"missing": "缺值", "unmapped": "未映射", "invalid": "无效值", "conflict": "多个不同值"}[value.Reason])
			}
			properties = append(properties, components.Property{ID: field.ID, Title: field.Name, Value: text})
		}
		components.PropertyGrid(c, properties, 2)
		return
	}
}
