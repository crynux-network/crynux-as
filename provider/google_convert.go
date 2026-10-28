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
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{
						"name": tc.Name,
						"args": args,
					},
				})
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
			parts := []map[string]any{{
				"functionResponse": map[string]any{
					"name": name,
					"response": map[string]any{
						"result": msg.Content,
					},
				},
			}}
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
	if cfg, omit := mapGoogleToolConfig(req.ToolChoice); cfg != nil {
		out["toolConfig"] = cfg
		if omit {
			delete(out, "tools")
		}
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
	if mime, schema := mapGoogleResponseFormat(req.ResponseFormat); mime != "" {
		gen["responseMimeType"] = mime
		if schema != nil {
			gen["responseSchema"] = schema
		}
	}
	if len(gen) > 0 {
		out["generationConfig"] = gen
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

func mapGoogleToolConfig(choice any) (map[string]any, bool) {
	if choice == nil {
		return nil, false
	}
	switch v := choice.(type) {
	case string:
		switch v {
		case "none":
			return map[string]any{
				"functionCallingConfig": map[string]any{"mode": "NONE"},
			}, true
		case "auto":
			return map[string]any{
				"functionCallingConfig": map[string]any{"mode": "AUTO"},
			}, false
		case "required":
			return map[string]any{
				"functionCallingConfig": map[string]any{"mode": "ANY"},
			}, false
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		if typ == "function" {
			fn, _ := v["function"].(map[string]any)
			name, _ := fn["name"].(string)
			return map[string]any{
				"functionCallingConfig": map[string]any{
					"mode":                 "ANY",
					"allowedFunctionNames": []string{name},
				},
			}, false
		}
	}
	raw, err := json.Marshal(choice)
	if err != nil {
		return nil, false
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return mapGoogleToolConfig(asString)
	}
	var asMap map[string]any
	if json.Unmarshal(raw, &asMap) == nil {
		return mapGoogleToolConfig(asMap)
	}
	return nil, false
}

func mapGoogleResponseFormat(responseFormat map[string]any) (string, any) {
	if responseFormat == nil {
		return "", nil
	}
	typ, _ := responseFormat["type"].(string)
	switch typ {
	case "json_object":
		return "application/json", nil
	case "json_schema":
		js, _ := responseFormat["json_schema"].(map[string]any)
		if js == nil {
			return "application/json", nil
		}
		return "application/json", js["schema"]
	default:
		return "", nil
	}
}

func parseGoogleGenerateContentResponse(raw []byte) (AssembledMessage, *NormalizedUsage, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
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
		for i, part := range cand.Content.Parts {
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
				msg.ToolCalls = append(msg.ToolCalls, AssembledToolCall{
					ID:        fmt.Sprintf("call_google_%d", i),
					Name:      part.FunctionCall.Name,
					Arguments: args,
				})
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
