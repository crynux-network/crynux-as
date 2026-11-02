package provider

import (
	"crynux_as/llmadapter"
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

const (
	CanonicalPartText  = "text"
	CanonicalPartImage = "image"
)

// CanonicalContentPart is one ordered text or base64 image content part.
type CanonicalContentPart struct {
	Type     string
	Text     string
	MIMEType string
	// Data is the raw base64 image payload without the data URL prefix.
	Data string
	// Detail is the OpenAI image detail value. Only the OpenAI converter uses it.
	Detail string
}

type CanonicalMessage struct {
	Role       string
	Parts      []CanonicalContentPart
	ToolCalls  []AssembledToolCall
	ToolCallID string
	Name       string
}

// TextOnlyParts returns a single text part, or no parts for an empty string.
func TextOnlyParts(s string) []CanonicalContentPart {
	if s == "" {
		return nil
	}
	return []CanonicalContentPart{{Type: CanonicalPartText, Text: s}}
}

// Text concatenates all text parts in order.
func (m CanonicalMessage) Text() string {
	var b strings.Builder
	for _, part := range m.Parts {
		if part.Type == CanonicalPartText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// HasImages reports whether the message contains at least one image part.
func (m CanonicalMessage) HasImages() bool {
	for _, part := range m.Parts {
		if part.Type == CanonicalPartImage {
			return true
		}
	}
	return false
}

type CanonicalTool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func ParseCanonicalRequest(requestJSON []byte, model string, apiType models.TaskAPIType, defaultMaxTokens int) (CanonicalRequest, error) {
	switch apiType {
	case models.TaskAPITypeResponses:
		return parseResponsesAsCanonical(requestJSON, model, defaultMaxTokens)
	default:
		return parseChatAsCanonical(requestJSON, model, defaultMaxTokens)
	}
}

func parseChatAsCanonical(requestJSON []byte, model string, defaultMaxTokens int) (CanonicalRequest, error) {
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
		MaxTokens:         defaultMaxTokens,
	}
	if raw.MaxTokens != nil && *raw.MaxTokens > 0 {
		out.MaxTokens = *raw.MaxTokens
	}
	if raw.MaxCompletionTokens != nil && *raw.MaxCompletionTokens > 0 {
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
	parts, err := parseContentParts(msg.Content)
	if err != nil {
		return CanonicalMessage{}, err
	}
	out := CanonicalMessage{
		Role:       msg.Role,
		Parts:      parts,
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

func parseResponsesAsCanonical(requestJSON []byte, model string, defaultMaxTokens int) (CanonicalRequest, error) {
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
		MaxTokens:         defaultMaxTokens,
	}
	if raw.MaxOutputTokens != nil && *raw.MaxOutputTokens > 0 {
		out.MaxTokens = *raw.MaxOutputTokens
	}
	if raw.Instructions != "" {
		out.Messages = append(out.Messages, CanonicalMessage{Role: "system", Parts: TextOnlyParts(raw.Instructions)})
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
		return []CanonicalMessage{{Role: "user", Parts: TextOnlyParts(v)}}, nil
	case []any:
		var out []CanonicalMessage
		for _, item := range v {
			msg, err := parseResponsesInputItem(item)
			if err != nil {
				return nil, err
			}
			if msg.Role != "" || len(msg.ToolCalls) > 0 || len(msg.Parts) > 0 {
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
				return []CanonicalMessage{{Role: "user", Parts: TextOnlyParts(s)}}, nil
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
		parts, err := parseAnyContentParts(obj["content"])
		if err != nil {
			return CanonicalMessage{}, err
		}
		return CanonicalMessage{Role: role, Parts: parts}, nil
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
		parts, err := parseAnyContentParts(obj["output"])
		if err != nil {
			return CanonicalMessage{}, err
		}
		return CanonicalMessage{Role: "tool", ToolCallID: callID, Parts: parts}, nil
	default:
		parts, err := parseAnyContentParts(obj["content"])
		if err != nil {
			return CanonicalMessage{}, err
		}
		if role == "" {
			role = "user"
		}
		return CanonicalMessage{Role: role, Parts: parts}, nil
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

// parseContentParts converts string or content-array JSON into ordered parts.
// Text parts come from text, input_text, and output_text. Image parts come from
// Chat Completions image_url objects and Responses input_image items. Other
// part types are ignored.
func parseContentParts(raw json.RawMessage) ([]CanonicalContentPart, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return TextOnlyParts(s), nil
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	var out []CanonicalContentPart
	for i, item := range items {
		var typ string
		_ = json.Unmarshal(item["type"], &typ)
		switch typ {
		case "text", "input_text", "output_text":
			var text string
			_ = json.Unmarshal(item["text"], &text)
			if text != "" {
				out = append(out, CanonicalContentPart{Type: CanonicalPartText, Text: text})
			}
		case "image_url":
			var image struct {
				URL    string `json:"url"`
				Detail string `json:"detail"`
			}
			if err := json.Unmarshal(item["image_url"], &image); err != nil {
				return nil, fmt.Errorf("content[%d].image_url must be an object with url", i)
			}
			part, err := parseCanonicalImagePart(image.URL, image.Detail)
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", i, err)
			}
			out = append(out, part)
		case "input_image":
			var url, detail string
			if err := json.Unmarshal(item["image_url"], &url); err != nil {
				return nil, fmt.Errorf("content[%d].image_url must be a base64 data URL string", i)
			}
			if raw, ok := item["detail"]; ok {
				_ = json.Unmarshal(raw, &detail)
			}
			part, err := parseCanonicalImagePart(url, detail)
			if err != nil {
				return nil, fmt.Errorf("content[%d]: %w", i, err)
			}
			out = append(out, part)
		}
	}
	return out, nil
}

func parseCanonicalImagePart(url, detail string) (CanonicalContentPart, error) {
	parsed, err := llmadapter.ParseImageDataURL(url)
	if err != nil {
		return CanonicalContentPart{}, err
	}
	return CanonicalContentPart{
		Type:     CanonicalPartImage,
		MIMEType: parsed.MIMEType,
		Data:     parsed.Data,
		Detail:   detail,
	}, nil
}

func parseAnyContentParts(v any) ([]CanonicalContentPart, error) {
	if v == nil {
		return nil, nil
	}
	if s, ok := v.(string); ok {
		return TextOnlyParts(s), nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return parseContentParts(raw)
}

// namedFunctionToolChoiceName extracts the function name from Chat Completions
// {"type":"function","function":{"name":N}} or Responses {"type":"function","name":N}.
func namedFunctionToolChoiceName(choice map[string]any) (string, error) {
	typ, _ := choice["type"].(string)
	if typ != "function" {
		return "", fmt.Errorf("unsupported tool_choice type %q", typ)
	}
	if fn, ok := choice["function"].(map[string]any); ok {
		if name, _ := fn["name"].(string); name != "" {
			return name, nil
		}
	}
	if name, _ := choice["name"].(string); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("tool_choice function name is required")
}
