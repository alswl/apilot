package workbench

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/alswl/apilot/connectors/protocol"
	"github.com/alswl/apilot/core/pkg/common"
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
	return d.config(false)
}

func (d *connectorJSONDraft) config(reuseEndpoint bool) (endpoint, scope map[string]any, ok bool) {
	var endpointOK, scopeOK bool
	endpointOK = true
	if !reuseEndpoint {
		endpoint, endpointOK = connectorJSONObject(d.endpoint)
	}
	scope, scopeOK = connectorJSONObject(d.scope)
	return endpoint, scope, endpointOK && scopeOK && common.ConnectorConfigAllowsOrdinaryValues(d.endpointSchema, endpoint) && common.ConnectorConfigAllowsOrdinaryValues(d.scopeSchema, scope)
}

func (d *connectorJSONDraft) choose(level, field, value string, reuseEndpoint ...bool) {
	endpoint, scope, ok := d.config(len(reuseEndpoint) > 0 && reuseEndpoint[0])
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
		if p.reuseEndpoint && config.level == "endpoint" {
			continue
		}
		if ui.TextArea(c.Key([3]string{config.level, "draft"}), config.draft).Label(config.label).FillWidth().Height(65).Disabled(p.saving).Changed() {
			p.activateOptions(connectorConfigField{})
		}
		if object, ok := connectorJSONObject(*config.draft); !ok {
			components.Muted(c.Key([3]string{config.level, "invalid"}), tr("请输入有效的 JSON 对象"))
		} else if !common.ConnectorConfigAllowsOrdinaryValues(config.schema, object) {
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
