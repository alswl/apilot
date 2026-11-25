package workbench

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/services"
	"github.com/alswl/apilot/desktop/internal/ui/components"
	"github.com/egoist/mygo/ui"
)

type connectorJSONDraft struct {
	endpoint, scope             string
	endpointSchema, scopeSchema *protocol.ConfigSchema
	showSchema                  bool
}

func newConnectorJSONDraft(plugin services.WorkbenchPlugin) *connectorJSONDraft {
	return &connectorJSONDraft{endpoint: "{}", scope: "{}", endpointSchema: plugin.EndpointConfig, scopeSchema: plugin.ScopeConfig}
}

func connectorJSONObject(raw string) (map[string]any, bool) {
	if len(raw) > protocol.MaxFrame/4 || !json.Valid([]byte(raw)) {
		return nil, false
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, false
	}
	return value, true
}

func (d *connectorJSONDraft) objects() (endpoint, scope map[string]any, ok bool) {
	var endpointOK, scopeOK bool
	endpoint, endpointOK = connectorJSONObject(d.endpoint)
	scope, scopeOK = connectorJSONObject(d.scope)
	return endpoint, scope, endpointOK && scopeOK && safeConnectorJSON(d.endpointSchema, endpoint) && safeConnectorJSON(d.scopeSchema, scope)
}

func (d *connectorJSONDraft) choose(level, field, value string) {
	endpoint, scope, ok := d.objects()
	if !ok {
		return
	}
	target, fields := &d.scope, scope
	if level == "endpoint" {
		target, fields = &d.endpoint, endpoint
	}
	fields[field] = value
	data, _ := json.MarshalIndent(fields, "", "  ")
	*target = string(data)
}

func (m *mockup) connectorJSONFields(c *ui.Context, p *connectorLoginPanel) {
	d := p.jsonDraft
	components.Muted(c.Key("hint"), tr("输入 JSON 对象。请通过登录提供凭据。"))
	ui.Checkbox(c.Key("show-schema"), &d.showSchema, tr("显示配置规则"))
	for _, config := range []struct {
		level, label string
		draft        *string
		schema       *protocol.ConfigSchema
	}{
		{"endpoint", tr("端点配置 JSON"), &d.endpoint, d.endpointSchema},
		{"scope", tr("范围配置 JSON"), &d.scope, d.scopeSchema},
	} {
		if ui.TextArea(c.Key([3]string{config.level, "draft"}), config.draft).Label(config.label).FillWidth().Height(65).Disabled(p.saving).Changed() {
			p.activateOptions(connectorConfigField{})
		}
		if object, ok := connectorJSONObject(*config.draft); !ok {
			components.Muted(c.Key([3]string{config.level, "invalid"}), tr("请输入有效的 JSON 对象"))
		} else if !safeConnectorJSON(config.schema, object) {
			components.Muted(c.Key([3]string{config.level, "invalid"}), tr("请从配置中移除凭据字段"))
		}
		if config.schema == nil {
			continue
		}
		if d.showSchema {
			var pretty bytes.Buffer
			if json.Indent(&pretty, config.schema.Schema, "", "  ") == nil {
				source := pretty.String()
				ui.TextArea(c.Key([3]string{config.level, "schema"}), &source).Label(config.label).ReadOnly(true).FillWidth().Height(90)
			}
		}
		for _, field := range slices.Sorted(maps.Keys(config.schema.Fields)) {
			meta := config.schema.Fields[field]
			if !meta.DynamicOptions {
				continue
			}
			title := meta.Title
			if title == "" {
				title = field
			}
			if ui.Button(c.Key([3]string{config.level, "option", field}), tr("查找选项")+" · "+title).Disabled(p.saving || !p.canSave()).Clicked() {
				p.activateOptions(connectorConfigField{key: field, level: config.level})
				m.loadConnectorChoices(p, "")
			}
		}
	}
}

type connectorJSONSchema struct {
	WriteOnly  bool                           `json:"writeOnly"`
	Format     string                         `json:"format"`
	Properties map[string]connectorJSONSchema `json:"properties"`
	Items      json.RawMessage                `json:"items"`
	AllOf      []connectorJSONSchema          `json:"allOf"`
	AnyOf      []connectorJSONSchema          `json:"anyOf"`
	OneOf      []connectorJSONSchema          `json:"oneOf"`
}

func safeConnectorJSON(config *protocol.ConfigSchema, data map[string]any) bool {
	if config == nil {
		return true
	}
	for key, field := range config.Fields {
		if _, present := data[key]; present && (field.Control == "password" || field.Control == "secret") {
			return false
		}
	}
	var schema connectorJSONSchema
	if json.Unmarshal(config.Schema, &schema) != nil {
		return true
	}
	return !schema.containsCredential(data)
}

func (s connectorJSONSchema) containsCredential(value any) bool {
	if s.WriteOnly || s.Format == "password" {
		return true
	}
	switch value := value.(type) {
	case map[string]any:
		for key, item := range value {
			if field, ok := s.Properties[key]; ok && field.containsCredential(item) {
				return true
			}
		}
	case []any:
		var item connectorJSONSchema
		if len(s.Items) > 0 && json.Unmarshal(s.Items, &item) == nil && slices.ContainsFunc(value, item.containsCredential) {
			return true
		}
	}
	for _, alternatives := range [][]connectorJSONSchema{s.AllOf, s.AnyOf, s.OneOf} {
		if slices.ContainsFunc(alternatives, func(branch connectorJSONSchema) bool { return branch.containsCredential(value) }) {
			return true
		}
	}
	return false
}
