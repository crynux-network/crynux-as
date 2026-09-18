package llmadapter

import (
	"encoding/json"
	"fmt"
)

const (
	maxJSONSchemaBytes      = 64 * 1024
	maxJSONSchemaDepth      = 32
	maxJSONSchemaProperties = 1024
)

func normalizeChatTools(tools []map[string]interface{}) ([]map[string]interface{}, error) {
	normalized := make([]map[string]interface{}, 0, len(tools))
	names := make(map[string]struct{}, len(tools))
	for i, tool := range tools {
		if err := rejectUnknownKeys(tool, "type", "function"); err != nil {
			return nil, newValidationError("tools", fmt.Sprintf("tools[%d]: %v", i, err))
		}
		if tool["type"] != "function" {
			return nil, newValidationError("tools", fmt.Sprintf("tools[%d].type must be function", i))
		}
		function, ok := tool["function"].(map[string]interface{})
		if !ok {
			return nil, newValidationError("tools", fmt.Sprintf("tools[%d].function is required", i))
		}
		if err := validateFunctionDefinition(function, fmt.Sprintf("tools[%d].function", i)); err != nil {
			return nil, err
		}
		name := function["name"].(string)
		if _, exists := names[name]; exists {
			return nil, newValidationError("tools", fmt.Sprintf("function name %q must be unique", name))
		}
		names[name] = struct{}{}
		normalized = append(normalized, tool)
	}
	return normalized, nil
}

func normalizeResponsesTools(tools []map[string]interface{}) ([]map[string]interface{}, error) {
	normalized := make([]map[string]interface{}, 0, len(tools))
	names := make(map[string]struct{}, len(tools))
	for i, tool := range tools {
		if err := rejectUnknownKeys(tool, "type", "name", "description", "parameters", "strict"); err != nil {
			return nil, newValidationError("tools", fmt.Sprintf("tools[%d]: %v", i, err))
		}
		if tool["type"] != "function" {
			return nil, newValidationError("tools", fmt.Sprintf("built-in tool type %q at index %d is not supported", tool["type"], i))
		}
		function := map[string]interface{}{}
		for _, key := range []string{"name", "description", "parameters", "strict"} {
			if value, ok := tool[key]; ok {
				function[key] = value
			}
		}
		if err := validateFunctionDefinition(function, fmt.Sprintf("tools[%d]", i)); err != nil {
			return nil, err
		}
		name := function["name"].(string)
		if _, exists := names[name]; exists {
			return nil, newValidationError("tools", fmt.Sprintf("function name %q must be unique", name))
		}
		names[name] = struct{}{}
		normalized = append(normalized, map[string]interface{}{
			"type":     "function",
			"function": function,
		})
	}
	return normalized, nil
}

func validateFunctionDefinition(function map[string]interface{}, field string) error {
	if err := rejectUnknownKeys(function, "name", "description", "parameters", "strict"); err != nil {
		return newValidationError("tools", fmt.Sprintf("%s: %v", field, err))
	}
	name, ok := function["name"].(string)
	if !ok || name == "" {
		return newValidationError("tools", fmt.Sprintf("%s.name is required", field))
	}
	if value, ok := function["description"]; ok {
		if _, valid := value.(string); !valid {
			return newValidationError("tools", fmt.Sprintf("%s.description must be a string", field))
		}
	}
	if value, ok := function["parameters"]; ok {
		schema, valid := value.(map[string]interface{})
		if !valid {
			return newValidationError("tools", fmt.Sprintf("%s.parameters must be an object", field))
		}
		if err := validateSchemaLimits(schema); err != nil {
			return newValidationError("tools", fmt.Sprintf("%s.parameters: %v", field, err))
		}
	}
	if value, ok := function["strict"]; ok {
		if _, valid := value.(bool); !valid {
			return newValidationError("tools", fmt.Sprintf("%s.strict must be a boolean", field))
		}
	}
	return nil
}

func normalizeToolChoice(value any, tools []map[string]interface{}, responsesStyle bool) (any, error) {
	if value == nil {
		if len(tools) == 0 {
			return "none", nil
		}
		return "auto", nil
	}
	if choice, ok := value.(string); ok {
		switch choice {
		case "none", "auto":
			return choice, nil
		case "required":
			if len(tools) == 0 {
				return nil, newValidationError("tool_choice", "required needs at least one tool")
			}
			return choice, nil
		default:
			return nil, newValidationError("tool_choice", "must be none, auto, required, or a named function")
		}
	}
	choice, ok := value.(map[string]interface{})
	if !ok {
		return nil, newValidationError("tool_choice", "must be a string or object")
	}

	var name string
	if responsesStyle {
		if err := rejectUnknownKeys(choice, "type", "name"); err != nil {
			return nil, newValidationError("tool_choice", err.Error())
		}
		if choice["type"] != "function" {
			return nil, newValidationError("tool_choice", "only function choices are supported")
		}
		name, _ = choice["name"].(string)
	} else {
		if err := rejectUnknownKeys(choice, "type", "function"); err != nil {
			return nil, newValidationError("tool_choice", err.Error())
		}
		if choice["type"] != "function" {
			return nil, newValidationError("tool_choice", "only function choices are supported")
		}
		function, valid := choice["function"].(map[string]interface{})
		if !valid {
			return nil, newValidationError("tool_choice", "function is required")
		}
		if err := rejectUnknownKeys(function, "name"); err != nil {
			return nil, newValidationError("tool_choice", fmt.Sprintf("function: %v", err))
		}
		name, _ = function["name"].(string)
	}
	if name == "" {
		return nil, newValidationError("tool_choice", "function name is required")
	}
	if countToolName(tools, name) != 1 {
		return nil, newValidationError("tool_choice", "named function must match exactly one declared tool")
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": name,
		},
	}, nil
}

func countToolName(tools []map[string]interface{}, name string) int {
	count := 0
	for _, tool := range tools {
		function, _ := tool["function"].(map[string]interface{})
		if function["name"] == name {
			count++
		}
	}
	return count
}

func normalizeChatResponseFormat(value any) (map[string]interface{}, error) {
	if value == nil {
		return nil, nil
	}
	format, ok := value.(map[string]interface{})
	if !ok {
		return nil, newValidationError("response_format", "must be an object")
	}
	if len(format) == 0 {
		return nil, nil
	}
	formatType, _ := format["type"].(string)
	switch formatType {
	case "text":
		if err := rejectUnknownKeys(format, "type"); err != nil {
			return nil, newValidationError("response_format", err.Error())
		}
		return nil, nil
	case "json_object":
		if err := rejectUnknownKeys(format, "type"); err != nil {
			return nil, newValidationError("response_format", err.Error())
		}
		return map[string]interface{}{"type": "json_object"}, nil
	case "json_schema":
		if err := rejectUnknownKeys(format, "type", "json_schema"); err != nil {
			return nil, newValidationError("response_format", err.Error())
		}
		definition, ok := format["json_schema"].(map[string]interface{})
		if !ok {
			return nil, newValidationError("response_format", "json_schema is required")
		}
		if err := validateJSONSchemaDefinition(definition); err != nil {
			return nil, err
		}
		return format, nil
	default:
		return nil, newValidationError("response_format", "type must be text, json_object, or json_schema")
	}
}

func normalizeResponsesTextFormat(value any) (map[string]interface{}, error) {
	if value == nil {
		return nil, nil
	}
	format, ok := value.(map[string]interface{})
	if !ok {
		return nil, newValidationError("text.format", "must be an object")
	}
	if len(format) == 0 {
		return nil, nil
	}
	formatType, _ := format["type"].(string)
	switch formatType {
	case "text":
		if err := rejectUnknownKeys(format, "type"); err != nil {
			return nil, newValidationError("text.format", err.Error())
		}
		return nil, nil
	case "json_object":
		if err := rejectUnknownKeys(format, "type"); err != nil {
			return nil, newValidationError("text.format", err.Error())
		}
		return map[string]interface{}{"type": "json_object"}, nil
	case "json_schema":
		if err := validateJSONSchemaDefinition(format); err != nil {
			return nil, err
		}
		definition := make(map[string]interface{}, len(format)-1)
		for key, item := range format {
			if key != "type" {
				definition[key] = item
			}
		}
		return map[string]interface{}{
			"type":        "json_schema",
			"json_schema": definition,
		}, nil
	default:
		return nil, newValidationError("text.format", "type must be text, json_object, or json_schema")
	}
}

func validateJSONSchemaDefinition(definition map[string]interface{}) error {
	if err := rejectUnknownKeys(definition, "type", "name", "description", "schema", "strict"); err != nil {
		return newValidationError("response_format", err.Error())
	}
	name, ok := definition["name"].(string)
	if !ok || name == "" {
		return newValidationError("response_format", "schema name is required")
	}
	schema, ok := definition["schema"].(map[string]interface{})
	if !ok {
		return newValidationError("response_format", "schema must be an object")
	}
	if err := validateSchemaLimits(schema); err != nil {
		return newValidationError("response_format", err.Error())
	}
	if value, ok := definition["description"]; ok {
		if _, valid := value.(string); !valid {
			return newValidationError("response_format", "description must be a string")
		}
	}
	if value, ok := definition["strict"]; ok {
		if _, valid := value.(bool); !valid {
			return newValidationError("response_format", "strict must be a boolean")
		}
	}
	return nil
}

func rejectUnknownKeys(value map[string]interface{}, allowed ...string) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key := range value {
		if _, ok := allowedSet[key]; !ok {
			return fmt.Errorf("field %q is not supported", key)
		}
	}
	return nil
}

func validateSchemaLimits(schema map[string]interface{}) error {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("schema must contain JSON-compatible values")
	}
	if len(encoded) > maxJSONSchemaBytes {
		return fmt.Errorf("schema exceeds the 65536-byte limit")
	}

	propertyCount := 0
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		if depth > maxJSONSchemaDepth {
			return fmt.Errorf("schema exceeds the maximum nesting depth of 32")
		}
		switch typed := value.(type) {
		case map[string]interface{}:
			if properties, ok := typed["properties"].(map[string]interface{}); ok {
				propertyCount += len(properties)
				if propertyCount > maxJSONSchemaProperties {
					return fmt.Errorf("schema exceeds the 1024-property limit")
				}
			}
			for _, child := range typed {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case []interface{}:
			for _, child := range typed {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(schema, 1)
}
