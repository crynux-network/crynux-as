package provider

import (
	"encoding/json"
	"fmt"
)

func convertCanonicalToGoogle(req CanonicalRequest) ([]byte, error) {
	out := map[string]any{}
	var systemText string
	contents := make([]map[string]any, 0, len(req.Messages))

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system", "developer":
			if msg.Content != "" {
				if systemText != "" {
					systemText += "\n\n"
				}
				systemText += msg.Content
			}
		case "assistant":
			parts := make([]map[string]any, 0)
			if msg.Content != "" {
				parts = append(parts, map[string]any{"text": msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				var args any = map[string]any{}
				if tc.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Arguments), &args)
				}
				fc := map[string]any{
					"name": tc.Name,
					"args": args,
				}
				if tc.ID != "" {
					fc["id"] = tc.ID
				}
				parts = append(parts, map[string]any{"functionCall": fc})
			}
			if len(parts) == 0 {
				parts = append(parts, map[string]any{"text": ""})
			}
			contents = append(contents, map[string]any{"role": "model", "parts": parts})
		case "tool":
			name := msg.Name
			if name == "" {
				name = lookupToolName(req.Messages, msg.ToolCallID)
			}
			fr := map[string]any{
				"name": name,
				"response": map[string]any{
					"result": msg.Content,
				},
			}
			if msg.ToolCallID != "" {
				fr["id"] = msg.ToolCallID
			}
			parts := []map[string]any{{"functionResponse": fr}}
			contents = append(contents, map[string]any{"role": "user", "parts": parts})
		default:
			contents = append(contents, map[string]any{
				"role":  "user",
				"parts": []map[string]any{{"text": msg.Content}},
			})
		}
	}
	if systemText != "" {
		out["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": systemText}},
		}
	}
	out["contents"] = contents

	if len(req.Tools) > 0 {
		decls := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			decls = append(decls, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			})
		}
		out["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}
	cfg, err := mapGoogleToolConfig(req.ToolChoice)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		out["toolConfig"] = cfg
	}

	gen := map[string]any{}
	if req.MaxTokens > 0 {
		gen["maxOutputTokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		gen["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		gen["topP"] = *req.TopP
	}
	if seqs := normalizeStopSequences(req.Stop); len(seqs) > 0 {
		gen["stopSequences"] = seqs
	}
	mime, schema, err := mapGoogleResponseFormat(req.ResponseFormat)
	if err != nil {
		return nil, err
	}
	if mime != "" {
		gen["responseMimeType"] = mime
		if schema != nil {
			gen["responseSchema"] = schema
		}
	}
	if len(gen) > 0 {
		out["generationConfig"] = gen
	}

	_, hasTools := out["tools"]
	if hasTools && mime == "application/json" {
		return nil, fmt.Errorf("google does not support tools together with JSON structured output")
	}
	return json.Marshal(out)
}

func lookupToolName(messages []CanonicalMessage, toolCallID string) string {
	for _, msg := range messages {
		for _, tc := range msg.ToolCalls {
			if tc.ID == toolCallID {
				return tc.Name
			}
		}
	}
	return toolCallID
}

func mapGoogleToolConfig(choice any) (map[string]any, error) {
	if choice == nil {
		return nil, nil
	}
	switch v := choice.(type) {
	case string:
		switch v {
		case "none":
			return map[string]any{
				"functionCallingConfig": map[string]any{"mode": "NONE"},
			}, nil
		case "auto":
			return map[string]any{
				"functionCallingConfig": map[string]any{"mode": "AUTO"},
			}, nil
		case "required":
			return map[string]any{
				"functionCallingConfig": map[string]any{"mode": "ANY"},
			}, nil
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
			return map[string]any{
				"functionCallingConfig": map[string]any{
					"mode":                 "ANY",
					"allowedFunctionNames": []string{name},
				},
			}, nil
		}
		return nil, fmt.Errorf("unsupported tool_choice type %q", typ)
	}
	raw, err := json.Marshal(choice)
	if err != nil {
		return nil, err
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return mapGoogleToolConfig(asString)
	}
	var asMap map[string]any
	if json.Unmarshal(raw, &asMap) == nil {
		return mapGoogleToolConfig(asMap)
	}
	return nil, fmt.Errorf("unsupported tool_choice")
}

func mapGoogleResponseFormat(responseFormat map[string]any) (string, any, error) {
	if responseFormat == nil {
		return "", nil, nil
	}
	typ, _ := responseFormat["type"].(string)
	switch typ {
	case "json_object":
		return "application/json", nil, nil
	case "json_schema":
		js, _ := responseFormat["json_schema"].(map[string]any)
		if js == nil {
			return "", nil, fmt.Errorf("response_format.json_schema is required")
		}
		schema, ok := js["schema"]
		if !ok || schema == nil {
			return "", nil, fmt.Errorf("response_format.json_schema.schema is required")
		}
		return "application/json", schema, nil
	case "text", "":
		return "", nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported response_format type %q", typ)
	}
}

func parseGoogleGenerateContentResponse(raw []byte) (AssembledMessage, *NormalizedUsage, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						ID   string         `json:"id"`
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount        uint64 `json:"promptTokenCount"`
			CandidatesTokenCount    uint64 `json:"candidatesTokenCount"`
			TotalTokenCount         uint64 `json:"totalTokenCount"`
			CachedContentTokenCount uint64 `json:"cachedContentTokenCount"`
			ThoughtsTokenCount      uint64 `json:"thoughtsTokenCount"`
			ToolUsePromptTokenCount uint64 `json:"toolUsePromptTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return AssembledMessage{}, nil, err
	}
	msg := AssembledMessage{}
	if len(resp.Candidates) > 0 {
		cand := resp.Candidates[0]
		msg.FinishReason = MapGoogleFinishReason(cand.FinishReason)
		toolIndex := 0
		for _, part := range cand.Content.Parts {
			if part.Text != "" {
				msg.Content += part.Text
			}
			if part.FunctionCall != nil {
				args := "{}"
				if part.FunctionCall.Args != nil {
					encoded, err := json.Marshal(part.FunctionCall.Args)
					if err != nil {
						return AssembledMessage{}, nil, fmt.Errorf("marshal function call args: %w", err)
					}
					args = string(encoded)
				}
				id := part.FunctionCall.ID
				if id == "" {
					id = fmt.Sprintf("call_google_%d", toolIndex)
				}
				msg.ToolCalls = append(msg.ToolCalls, AssembledToolCall{
					ID:        id,
					Name:      part.FunctionCall.Name,
					Arguments: args,
				})
				toolIndex++
			}
		}
	}
	if msg.FinishReason == "" {
		if len(msg.ToolCalls) > 0 {
			msg.FinishReason = "tool_calls"
		} else {
			msg.FinishReason = "stop"
		}
	} else if len(msg.ToolCalls) > 0 && msg.FinishReason == "stop" {
		msg.FinishReason = "tool_calls"
	}
	usage := &NormalizedUsage{
		InputTokens:         resp.UsageMetadata.PromptTokenCount,
		OutputTokens:        resp.UsageMetadata.CandidatesTokenCount,
		CachedInputTokens:   resp.UsageMetadata.CachedContentTokenCount,
		ReasoningTokens:     resp.UsageMetadata.ThoughtsTokenCount,
		ToolUsePromptTokens: resp.UsageMetadata.ToolUsePromptTokenCount,
		TotalTokens:         resp.UsageMetadata.TotalTokenCount,
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return msg, usage, nil
}
