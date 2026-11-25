package common

import (
	"encoding/json"
	"github.com/alswl/apilot/connectors/protocol"
	"slices"
)

const WorkbenchConnectorOptionsConfigLimit = 4096

type connectorJSONSchema struct {
	WriteOnly  bool                           `json:"writeOnly"`
	Format     string                         `json:"format"`
	Properties map[string]connectorJSONSchema `json:"properties"`
	Items      json.RawMessage                `json:"items"`
	AllOf      []connectorJSONSchema          `json:"allOf"`
	AnyOf      []connectorJSONSchema          `json:"anyOf"`
	OneOf      []connectorJSONSchema          `json:"oneOf"`
}

// ConnectorConfigAllowsOrdinaryValues rejects fields declared as credentials.
func ConnectorConfigAllowsOrdinaryValues(config *protocol.ConfigSchema, data map[string]any) bool {
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
