package provider

import (
	"encoding/json"
	"fmt"
)

func convertCanonicalToAnthropic(req CanonicalRequest) ([]byte, error) {
	out := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"stream":     req.Stream,
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if req.Stop != nil {
		out["stop_sequences"] = normalizeStopSequences(req.Stop)
	}

	var systemParts []string
	messages := make([]map[string]any, 0, len(req.Messages))
	pendingToolResults := make([]map[string]any, 0)

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": pendingToolResults,
		})
		pendingToolResults = nil
	}

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer":
			flushToolResults()
			if msg.Content != "" {
				systemParts = append(systemParts, msg.Content)
			}
		case "tool":
			block := map[string]any{
				"type":        "tool_result",
				"tool_use_id": msg.ToolCallID,
				"content":     msg.Content,
			}
			pendingToolResults = append(pendingToolResults, block)
		case "assistant":
			flushToolResults()
			content := make([]map[string]any, 0)
			if msg.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var input any = map[string]any{}
				if tc.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
						input = map[string]any{}
					}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			if len(content) == 0 {
				content = append(content, map[string]any{"type": "text", "text": ""})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": content})
		default: // user
			flushToolResults()
			messages = append(messages, map[string]any{
				"role":    "user",
				"content": msg.Content,
			})
		}
	}
	flushToolResults()

	if len(systemParts) > 0 {
		out["system"] = joinSystem(systemParts)
	}
	out["messages"] = messages

	omitTools := false
	if req.ToolChoice != nil {
		choice, omit, err := mapAnthropicToolChoice(req.ToolChoice)
		if err != nil {
			return nil, err
		}
		omitTools = omit
		if choice != nil {
			out["tool_choice"] = choice
		}
	}
	if !omitTools && len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Parameters,
			})
		}
		out["tools"] = tools
	}
	if format := mapAnthropicOutputConfig(req.ResponseFormat); format != nil {
		out["output_config"] = map[string]any{"format": format}
	}
	return json.Marshal(out)
}

func joinSystem(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n\n"
		}
		out += p
	}
	return out
}

func normalizeStopSequences(stop any) []string {
	switch v := stop.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	default:
		return nil
	}
}

func mapAnthropicToolChoice(choice any) (any, bool, error) {
	switch v := choice.(type) {
	case string:
		switch v {
		case "none":
			return nil, true, nil
		case "auto":
			return map[string]any{"type": "auto"}, false, nil
		case "required":
			return map[string]any{"type": "any"}, false, nil
		default:
			return nil, false, fmt.Errorf("unsupported tool_choice %q", v)
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		if typ == "function" {
			fn, _ := v["function"].(map[string]any)
			name, _ := fn["name"].(string)
			return map[string]any{"type": "tool", "name": name}, false, nil
		}
		return v, false, nil
	default:
		raw, err := json.Marshal(choice)
		if err != nil {
			return nil, false, err
		}
		var asMap map[string]any
		if err := json.Unmarshal(raw, &asMap); err == nil {
			return mapAnthropicToolChoice(asMap)
		}
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			return mapAnthropicToolChoice(asString)
		}
		return nil, false, fmt.Errorf("unsupported tool_choice")
	}
}

func mapAnthropicOutputConfig(responseFormat map[string]any) map[string]any {
	if responseFormat == nil {
		return nil
	}
	typ, _ := responseFormat["type"].(string)
	switch typ {
	case "json_object":
		return map[string]any{"type": "json_object"}
	case "json_schema":
		js, _ := responseFormat["json_schema"].(map[string]any)
		if js == nil {
			return nil
		}
		format := map[string]any{"type": "json_schema"}
		if name, ok := js["name"]; ok {
			format["name"] = name
		}
		if schema, ok := js["schema"]; ok {
			format["schema"] = schema
		}
		if strict, ok := js["strict"]; ok {
			format["strict"] = strict
		}
		return format
	default:
		return nil
	}
}

func parseAnthropicMessageResponse(raw []byte) (AssembledMessage, *NormalizedUsage, error) {
	var resp struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens              uint64 `json:"input_tokens"`
			OutputTokens             uint64 `json:"output_tokens"`
			CacheReadInputTokens     uint64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens uint64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return AssembledMessage{}, nil, err
	}
	msg := AssembledMessage{FinishReason: MapAnthropicStopReason(resp.StopReason)}
	for _, part := range resp.Content {
		switch part.Type {
		case "text":
			msg.Content += part.Text
		case "tool_use":
			args := "{}"
			if len(part.Input) > 0 {
				args = string(part.Input)
			}
			msg.ToolCalls = append(msg.ToolCalls, AssembledToolCall{
				ID:        part.ID,
				Name:      part.Name,
				Arguments: args,
			})
		}
	}
	usage := &NormalizedUsage{
		InputTokens:           resp.Usage.InputTokens,
		OutputTokens:          resp.Usage.OutputTokens,
		CachedInputTokens:     resp.Usage.CacheReadInputTokens,
		CacheWriteInputTokens: resp.Usage.CacheCreationInputTokens,
		TotalTokens:           resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}
	return msg, usage, nil
}
