package provider

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"strings"
)

// CanonicalRequest is the OpenAI Chat Completions shaped request used as the
// conversion source for Anthropic and Google adapters.
type CanonicalRequest struct {
	Model               string
	Messages            []CanonicalMessage
	Tools               []CanonicalTool
	ToolChoice          any
	MaxTokens           int
	Temperature         *float64
	TopP                *float64
	Stop                any
	Stream              bool
	ResponseFormat      map[string]any
	ParallelToolCalls   *bool
}

type CanonicalMessage struct {
	Role       string
	Content    string
	ToolCalls  []AssembledToolCall
	ToolCallID string
	Name       string
}

type CanonicalTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func ParseCanonicalRequest(requestJSON []byte, model string, apiType models.TaskAPIType) (CanonicalRequest, error) {
	switch apiType {
	case models.TaskAPITypeResponses:
		return parseResponsesAsCanonical(requestJSON, model)
	default:
		return parseChatAsCanonical(requestJSON, model)
	}
}

func parseChatAsCanonical(requestJSON []byte, model string) (CanonicalRequest, error) {
	var raw struct {
		Messages            []json.RawMessage `json:"messages"`
		Tools               []json.RawMessage `json:"tools"`
		ToolChoice          any               `json:"tool_choice"`
		MaxTokens           *int              `json:"max_tokens"`
		MaxCompletionTokens *int              `json:"max_completion_tokens"`
		Temperature         *float64          `json:"temperature"`
		TopP                *float64          `json:"top_p"`
		Stop                any               `json:"stop"`
		Stream              bool              `json:"stream"`
		ResponseFormat      map[string]any    `json:"response_format"`
		ParallelToolCalls   *bool             `json:"parallel_tool_calls"`
	}
	if err := json.Unmarshal(requestJSON, &raw); err != nil {
		return CanonicalRequest{}, err
	}
	out := CanonicalRequest{
		Model:             model,
		ToolChoice:        raw.ToolChoice,
		Temperature:       raw.Temperature,
		TopP:              raw.TopP,
		Stop:              raw.Stop,
		Stream:            raw.Stream,
		ResponseFormat:    raw.ResponseFormat,
		ParallelToolCalls: raw.ParallelToolCalls,
		MaxTokens:         1024,
	}
	if raw.MaxTokens != nil {
		out.MaxTokens = *raw.MaxTokens
	}
	if raw.MaxCompletionTokens != nil {
		out.MaxTokens = *raw.MaxCompletionTokens
	}
	for _, item := range raw.Messages {
		msg, err := parseChatMessage(item)
		if err != nil {
			return CanonicalRequest{}, err
		}
		out.Messages = append(out.Messages, msg)
	}
	for _, item := range raw.Tools {
		tool, err := parseChatTool(item)
		if err != nil {
			return CanonicalRequest{}, err
		}
		out.Tools = append(out.Tools, tool)
	}
	return out, nil
}

func parseChatMessage(raw json.RawMessage) (CanonicalMessage, error) {
	var msg struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		Name       string          `json:"name"`
		ToolCallID string          `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return CanonicalMessage{}, err
	}
	content, err := flattenContent(msg.Content)
	if err != nil {
		return CanonicalMessage{}, err
	}
	out := CanonicalMessage{
		Role:       msg.Role,
		Content:    content,
		Name:       msg.Name,
		ToolCallID: msg.ToolCallID,
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, AssembledToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return out, nil
}

func parseChatTool(raw json.RawMessage) (CanonicalTool, error) {
	var tool struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &tool); err != nil {
		return CanonicalTool{}, err
	}
	if tool.Type != "" && tool.Type != "function" {
		return CanonicalTool{}, fmt.Errorf("unsupported tool type %q", tool.Type)
	}
	params := tool.Function.Parameters
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return CanonicalTool{
		Name:        tool.Function.Name,
		Description: tool.Function.Description,
		Parameters:  params,
	}, nil
}

func parseResponsesAsCanonical(requestJSON []byte, model string) (CanonicalRequest, error) {
	var raw struct {
		Input             any            `json:"input"`
		Instructions      string         `json:"instructions"`
		Tools             []json.RawMessage `json:"tools"`
		ToolChoice        any            `json:"tool_choice"`
		MaxOutputTokens   *int           `json:"max_output_tokens"`
		Temperature       *float64       `json:"temperature"`
		TopP              *float64       `json:"top_p"`
		Stream            bool           `json:"stream"`
		Text              map[string]any `json:"text"`
		ParallelToolCalls *bool          `json:"parallel_tool_calls"`
	}
	if err := json.Unmarshal(requestJSON, &raw); err != nil {
		return CanonicalRequest{}, err
	}
	out := CanonicalRequest{
		Model:             model,
		ToolChoice:        raw.ToolChoice,
		Temperature:       raw.Temperature,
		TopP:              raw.TopP,
		Stream:            raw.Stream,
		ParallelToolCalls: raw.ParallelToolCalls,
		MaxTokens:         1024,
	}
	if raw.MaxOutputTokens != nil {
		out.MaxTokens = *raw.MaxOutputTokens
	}
	if raw.Instructions != "" {
		out.Messages = append(out.Messages, CanonicalMessage{Role: "system", Content: raw.Instructions})
	}
	if format := responsesTextFormat(raw.Text); format != nil {
		out.ResponseFormat = format
	}
	msgs, err := parseResponsesInput(raw.Input)
	if err != nil {
		return CanonicalRequest{}, err
	}
	out.Messages = append(out.Messages, msgs...)
	for _, item := range raw.Tools {
		tool, err := parseResponsesTool(item)
		if err != nil {
			return CanonicalRequest{}, err
		}
		out.Tools = append(out.Tools, tool)
	}
	return out, nil
}

func responsesTextFormat(text map[string]any) map[string]any {
	if text == nil {
		return nil
	}
	format, _ := text["format"].(map[string]any)
	if format == nil {
		return nil
	}
	typ, _ := format["type"].(string)
	switch typ {
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "json_schema":
		name, _ := format["name"].(string)
		schema, _ := format["schema"].(map[string]any)
		strict, _ := format["strict"].(bool)
		return map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   name,
				"schema": schema,
				"strict": strict,
			},
		}
	default:
		return nil
	}
}

func parseResponsesInput(input any) ([]CanonicalMessage, error) {
	if input == nil {
		return nil, nil
	}
	switch v := input.(type) {
	case string:
		return []CanonicalMessage{{Role: "user", Content: v}}, nil
	case []any:
		var out []CanonicalMessage
		for _, item := range v {
			msg, err := parseResponsesInputItem(item)
			if err != nil {
				return nil, err
			}
			if msg.Role != "" || len(msg.ToolCalls) > 0 || msg.Content != "" {
				out = append(out, msg)
			}
		}
		return out, nil
	default:
		raw, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		var items []any
		if err := json.Unmarshal(raw, &items); err != nil {
			var s string
			if err2 := json.Unmarshal(raw, &s); err2 == nil {
				return []CanonicalMessage{{Role: "user", Content: s}}, nil
			}
			return nil, err
		}
		return parseResponsesInput(items)
	}
}

func parseResponsesInputItem(item any) (CanonicalMessage, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return CanonicalMessage{}, err
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return CanonicalMessage{}, err
	}
	typ, _ := obj["type"].(string)
	role, _ := obj["role"].(string)
	switch typ {
	case "message", "":
		if role == "" {
			role = "user"
		}
		content, _ := flattenAnyContent(obj["content"])
		return CanonicalMessage{Role: role, Content: content}, nil
	case "function_call":
		id, _ := obj["call_id"].(string)
		if id == "" {
			id, _ = obj["id"].(string)
		}
		name, _ := obj["name"].(string)
		args, _ := obj["arguments"].(string)
		return CanonicalMessage{
			Role: "assistant",
			ToolCalls: []AssembledToolCall{{
				ID: id, Name: name, Arguments: args,
			}},
		}, nil
	case "function_call_output":
		callID, _ := obj["call_id"].(string)
		output, _ := flattenAnyContent(obj["output"])
		return CanonicalMessage{Role: "tool", ToolCallID: callID, Content: output}, nil
	default:
		content, _ := flattenAnyContent(obj["content"])
		if role == "" {
			role = "user"
		}
		return CanonicalMessage{Role: role, Content: content}, nil
	}
}

func parseResponsesTool(raw json.RawMessage) (CanonicalTool, error) {
	var tool struct {
		Type        string         `json:"type"`
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &tool); err != nil {
		return CanonicalTool{}, err
	}
	if tool.Type != "" && tool.Type != "function" {
		return CanonicalTool{}, fmt.Errorf("unsupported tool type %q", tool.Type)
	}
	params := tool.Parameters
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return CanonicalTool{
		Name:        tool.Name,
		Description: tool.Description,
		Parameters:  params,
	}, nil
}

func flattenContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, part := range parts {
		typ, _ := part["type"].(string)
		switch typ {
		case "text", "input_text", "output_text":
			if t, ok := part["text"].(string); ok {
				b.WriteString(t)
			}
		}
	}
	return b.String(), nil
}

func flattenAnyContent(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	switch t := v.(type) {
	case string:
		return t, nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return flattenContent(raw)
	}
}
