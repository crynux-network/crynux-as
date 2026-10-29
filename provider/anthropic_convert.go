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

	if len(req.Tools) > 0 {
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

	choice, err := mapAnthropicToolChoice(req.ToolChoice, req.ParallelToolCalls)
	if err != nil {
		return nil, err
	}
	if choice != nil {
		out["tool_choice"] = choice
	}

	format, err := mapAnthropicOutputConfig(req.ResponseFormat)
	if err != nil {
		return nil, err
	}
	if format != nil {
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

func mapAnthropicToolChoice(choice any, parallelToolCalls *bool) (any, error) {
	disableParallel := parallelToolCalls != nil && !*parallelToolCalls

	applyParallel := func(m map[string]any) map[string]any {
		if disableParallel {
			m["disable_parallel_tool_use"] = true
		}
		return m
	}

	if choice == nil {
		if disableParallel {
			return applyParallel(map[string]any{"type": "auto"}), nil
		}
		return nil, nil
	}

	switch v := choice.(type) {
	case string:
		switch v {
		case "none":
			return map[string]any{"type": "none"}, nil
		case "auto":
			return applyParallel(map[string]any{"type": "auto"}), nil
		case "required":
			return applyParallel(map[string]any{"type": "any"}), nil
		default:
			return nil, fmt.Errorf("unsupported tool_choice %q", v)
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		if typ == "function" {
			name, err := namedFunctionToolChoiceName(v)
			if err != nil {
				return nil, err
			}
			return applyParallel(map[string]any{"type": "tool", "name": name}), nil
		}
		if typ == "none" || typ == "auto" || typ == "any" || typ == "tool" {
			out := map[string]any{}
			for k, val := range v {
				out[k] = val
			}
			if typ != "none" {
				applyParallel(out)
			}
			return out, nil
		}
		return nil, fmt.Errorf("unsupported tool_choice type %q", typ)
	default:
		raw, err := json.Marshal(choice)
		if err != nil {
			return nil, err
		}
		var asMap map[string]any
		if err := json.Unmarshal(raw, &asMap); err == nil {
			return mapAnthropicToolChoice(asMap, parallelToolCalls)
		}
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			return mapAnthropicToolChoice(asString, parallelToolCalls)
		}
		return nil, fmt.Errorf("unsupported tool_choice")
	}
}

func mapAnthropicOutputConfig(responseFormat map[string]any) (map[string]any, error) {
	if responseFormat == nil {
		return nil, nil
	}
	typ, _ := responseFormat["type"].(string)
	switch typ {
	case "json_object":
		return map[string]any{
			"type":   "json_schema",
			"schema": map[string]any{"type": "object"},
		}, nil
	case "json_schema":
		js, _ := responseFormat["json_schema"].(map[string]any)
		if js == nil {
			return nil, fmt.Errorf("response_format.json_schema is required")
		}
		schema, ok := js["schema"]
		if !ok || schema == nil {
			return nil, fmt.Errorf("response_format.json_schema.schema is required")
		}
		return map[string]any{
			"type":   "json_schema",
			"schema": schema,
		}, nil
	case "text", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported response_format type %q", typ)
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
