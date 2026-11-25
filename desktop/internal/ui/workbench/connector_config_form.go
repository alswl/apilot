package workbench

import (
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorConfigField struct {
	key, kind, level string
	meta             protocol.ConfigField
	required         bool
	choices          []any
	initial          any
}

type connectorConfigForm struct {
	fields          []connectorConfigField
	endpoint, scope map[string]any
	primary         string
}

func newConnectorConfigForm(plugin services.WorkbenchPlugin) *connectorConfigForm {
	if plugin.EndpointConfig == nil || plugin.ScopeConfig == nil {
		return nil
	}
	result := &connectorConfigForm{}
	for _, config := range []struct {
		level  string
		schema *protocol.ConfigSchema
	}{{"endpoint", plugin.EndpointConfig}, {"scope", plugin.ScopeConfig}} {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		var root map[string]json.RawMessage
		if json.Unmarshal(config.schema.Schema, &schema) != nil || json.Unmarshal(config.schema.Schema, &root) != nil {
			return nil
		}
		for key := range root {
			switch key {
			case "type", "properties", "required", "additionalProperties", "title", "description", "$schema":
			default:
				return nil
			}
		}
		for _, key := range schema.Required {
			if schema.Properties[key] == nil {
				return nil
			}
		}
		for _, key := range slices.Sorted(maps.Keys(schema.Properties)) {
			var property struct {
				Type      string `json:"type"`
				Title     string `json:"title"`
				Enum      []any  `json:"enum"`
				WriteOnly bool   `json:"writeOnly"`
				ReadOnly  bool   `json:"readOnly"`
				Default   any    `json:"default"`
			}
			if json.Unmarshal(schema.Properties[key], &property) != nil || property.WriteOnly || property.ReadOnly {
				return nil
			}
			field := connectorConfigField{key: key, kind: property.Type, level: config.level, meta: config.schema.Fields[key],
				required: slices.Contains(schema.Required, key), choices: property.Enum, initial: property.Default}
			if field.meta.Title == "" {
				field.meta.Title = property.Title
			}
			if field.meta.Title == "" {
				field.meta.Title = key
			}
			switch field.kind {
			case "string", "integer", "number", "boolean":
			default:
				return nil
			}
			if field.meta.Control == "password" || field.meta.Control == "secret" || field.meta.DynamicOptions && field.kind != "string" {
				return nil
			}
			result.fields = append(result.fields, field)
		}
		for name := range config.schema.Fields {
			if schema.Properties[name] == nil {
				return nil
			}
		}
	}
	ordered := make([]connectorConfigField, 0, len(result.fields))
	pending := slices.Clone(result.fields)
	for len(pending) > 0 {
		index := slices.IndexFunc(pending, func(field connectorConfigField) bool {
			return slices.IndexFunc(field.meta.DependsOn, func(dependency string) bool {
				return slices.IndexFunc(ordered, func(prior connectorConfigField) bool { return prior.matchesDependency(field, dependency) }) < 0
			}) < 0
		})
		if index < 0 {
			return nil
		}
		ordered = append(ordered, pending[index])
		pending = slices.Delete(pending, index, index+1)
	}
	result.fields = ordered
	for _, field := range result.fields {
		if field.level == "scope" && field.required && field.meta.DynamicOptions && !slices.ContainsFunc(result.fields, func(later connectorConfigField) bool {
			return slices.ContainsFunc(later.meta.DependsOn, func(dep string) bool { return field.matchesDependency(later, dep) })
		}) {
			result.primary = field.key
		}
	}
	result.reset()
	return result
}

func (f connectorConfigField) matchesDependency(dependent connectorConfigField, name string) bool {
	return name == f.level+"."+f.key || name == f.key && (f.level == dependent.level || f.level == "endpoint" && dependent.level == "scope")
}

func (f *connectorConfigForm) reset() {
	f.endpoint, f.scope = map[string]any{}, map[string]any{}
	for _, field := range f.fields {
		if field.initial != nil {
			f.values(field.level)[field.key] = field.initial
		}
	}
}

func (f *connectorConfigForm) values(level string) map[string]any {
	if level == "endpoint" {
		return f.endpoint
	}
	return f.scope
}

func (f *connectorConfigForm) ready(field connectorConfigField) bool {
	for _, dep := range field.meta.DependsOn {
		if !slices.ContainsFunc(f.fields, func(prior connectorConfigField) bool {
			return prior.matchesDependency(field, dep) && f.values(prior.level)[prior.key] != nil
		}) {
			return false
		}
	}
	return true
}

func (f *connectorConfigForm) complete() bool {
	for _, field := range f.fields {
		if field.required && !(field.level == "scope" && field.key == f.primary) && f.values(field.level)[field.key] == nil {
			return false
		}
	}
	return true
}

func (f *connectorConfigForm) update(field connectorConfigField, value any) []connectorConfigField {
	if value == nil {
		delete(f.values(field.level), field.key)
	} else {
		f.values(field.level)[field.key] = value
	}
	affected := []connectorConfigField{field}
	for _, dependent := range f.fields {
		if slices.ContainsFunc(dependent.meta.DependsOn, func(dep string) bool {
			return slices.ContainsFunc(affected, func(prior connectorConfigField) bool { return prior.matchesDependency(dependent, dep) })
		}) {
			delete(f.values(dependent.level), dependent.key)
			affected = append(affected, dependent)
		}
	}
	return affected[1:]
}

func (m *mockup) connectorDraftFields(c *ui.Context, p *connectorLoginPanel) {
	for _, field := range p.form.fields {
		if field.level == "scope" && field.key == p.form.primary {
			continue
		}
		key := c.Key(field.level + "." + field.key)
		value := p.form.values(field.level)[field.key]
		disabled := p.saving || !p.form.ready(field)
		if field.meta.DynamicOptions {
			label := field.meta.Title
			if value != nil {
				label += " · " + configFieldText(value)
			}
			if ui.Button(key, label).Disabled(disabled).Clicked() {
				p.activateOptions(field)
				m.loadConnectorChoices(p, "")
			}
			continue
		}
		var changed bool
		switch {
		case len(field.choices) > 0 || field.kind == "boolean":
			values := field.choices
			if len(values) == 0 {
				values = []any{true, false}
			}
			labels := []string{tr("请选择")}
			selected := labels[0]
			for _, choice := range values {
				label := configFieldText(choice)
				labels = append(labels, label)
				if configFieldText(value) == label {
					selected = label
				}
			}
			components.Muted(key.Key("label"), field.meta.Title)
			changed = ui.Select(key.Key("value"), &selected, labels).Disabled(disabled).Changed()
			if changed {
				value = nil
				if index := slices.Index(labels, selected); index > 0 {
					value = values[index-1]
				}
			}
		default:
			text, exists := p.fieldDrafts[field.level+"."+field.key]
			if !exists {
				text = configFieldText(value)
			}
			changed = components.TextField(key, field.meta.Title, &text).Disabled(disabled).Changed()
			if changed {
				p.fieldDrafts[field.level+"."+field.key] = text
				value = parseConfigField(field.kind, text)
			}
		}
		if changed {
			p.updateDraft(field, value)
			clear(p.selected)
			p.activatePrimary()
			if p.optionField != "" {
				m.loadConnectorChoices(p, "")
			}
		}
	}
}

func configFieldText(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case bool:
		if value {
			return tr("是")
		}
		return tr("否")
	default:
		data, _ := json.Marshal(value)
		return string(data)
	}
}

func parseConfigField(kind, text string) any {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	switch kind {
	case "integer":
		if value, err := strconv.ParseInt(text, 10, 64); err == nil {
			return value
		}
	case "number":
		if value, err := strconv.ParseFloat(text, 64); err == nil && json.Valid([]byte(text)) {
			return value
		}
	default:
		return text
	}
	return nil
}

func (p *connectorLoginPanel) activateOptions(field connectorConfigField) {
	p.optionField, p.optionLevel, p.search, p.next = field.key, field.level, "", ""
	p.choices = nil
	clear(p.connected)
	p.queryGeneration++
	if p.cancelQuery != nil {
		p.cancelQuery()
		p.cancelQuery = nil
	}
	p.busy = false
}

func (p *connectorLoginPanel) activatePrimary() {
	p.activateOptions(connectorConfigField{})
	for _, field := range p.form.fields {
		if field.level == "scope" && field.key == p.form.primary && p.form.ready(field) {
			p.activateOptions(field)
			return
		}
	}
}

func (p *connectorLoginPanel) chooseDraftOption(value string) {
	for _, field := range p.form.fields {
		if field.key == p.optionField && field.level == p.optionLevel {
			p.updateDraft(field, value)
			break
		}
	}
	clear(p.selected)
	p.activatePrimary()
}

func (p *connectorLoginPanel) updateDraft(field connectorConfigField, value any) {
	for _, dependent := range p.form.update(field, value) {
		delete(p.fieldDrafts, dependent.level+"."+dependent.key)
	}
}
