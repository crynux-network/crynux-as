package provider

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"fmt"
)

func convertCanonicalToOpenAIChat(req CanonicalRequest) ([]byte, error) {
	out := map[string]any{
		"model":  req.Model,
		"stream": req.Stream,
	}
	if req.MaxTokens > 0 {
		out["max_completion_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if req.Stop != nil {
		out["stop"] = req.Stop
	}
	if req.ResponseFormat != nil {
		out["response_format"] = req.ResponseFormat
	}
	if req.ParallelToolCalls != nil {
		out["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	if req.Stream {
		out["stream_options"] = map[string]bool{"include_usage": true}
	}

	messages := make([]map[string]any, 0, len(req.Messages))
	for _, msg := range req.Messages {
		item := map[string]any{"role": msg.Role}
		if msg.Name != "" {
			item["name"] = msg.Name
		}
		switch msg.Role {
		case "tool":
			item["tool_call_id"] = msg.ToolCallID
			item["content"] = msg.Text()
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				toolCalls := make([]map[string]any, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					toolCalls = append(toolCalls, map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": tc.Arguments,
						},
					})
				}
				item["tool_calls"] = toolCalls
				if text := msg.Text(); text == "" {
					item["content"] = nil
				} else {
					item["content"] = text
				}
			} else {
				item["content"] = msg.Text()
			}
		default:
			item["content"] = openAIChatContent(msg)
		}
		messages = append(messages, item)
	}
	out["messages"] = messages

	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			params := t.Parameters
			if params == nil {
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  params,
				},
			})
		}
		out["tools"] = tools
	}

	choice, err := mapOpenAIChatToolChoice(req.ToolChoice)
	if err != nil {
		return nil, err
	}
	if choice != nil {
		out["tool_choice"] = choice
	}
	return json.Marshal(out)
}

// openAIChatContent returns a plain string for text-only messages and an ordered
// Chat Completions content array with image_url data URLs otherwise. The image
// detail value is passed through unchanged when present.
func openAIChatContent(msg CanonicalMessage) any {
	if !msg.HasImages() {
		return msg.Text()
	}
	content := make([]map[string]any, 0, len(msg.Parts))
	for _, part := range msg.Parts {
		switch part.Type {
		case CanonicalPartText:
			content = append(content, map[string]any{"type": "text", "text": part.Text})
		case CanonicalPartImage:
			imageURL := map[string]any{"url": llmadapter.FormatImageDataURL(part.MIMEType, part.Data)}
			if part.Detail != "" {
				imageURL["detail"] = part.Detail
			}
			content = append(content, map[string]any{"type": "image_url", "image_url": imageURL})
		}
	}
	return content
}

func mapOpenAIChatToolChoice(choice any) (any, error) {
	if choice == nil {
		return nil, nil
	}
	switch v := choice.(type) {
	case string:
		switch v {
		case "auto", "none", "required":
			return v, nil
		default:
			return nil, fmt.Errorf("unsupported tool_choice %q", v)
		}
	case map[string]any:
		typ, _ := v["type"].(string)
		switch typ {
		case "auto", "none", "required":
			return typ, nil
		case "function":
			name, err := namedFunctionToolChoiceName(v)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": name,
				},
			}, nil
		default:
			return nil, fmt.Errorf("unsupported tool_choice type %q", typ)
		}
	default:
		raw, err := json.Marshal(choice)
		if err != nil {
			return nil, err
		}
		var asMap map[string]any
		if err := json.Unmarshal(raw, &asMap); err == nil {
			return mapOpenAIChatToolChoice(asMap)
		}
		var asString string
		if err := json.Unmarshal(raw, &asString); err == nil {
			return mapOpenAIChatToolChoice(asString)
		}
		return nil, fmt.Errorf("unsupported tool_choice")
	}
}

func parseOpenAIChatCompletionResponse(raw []byte) (AssembledMessage, string, string, int64, *NormalizedUsage, error) {
	var resp struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Created int64  `json:"created"`
		Choices []struct {
			Message struct {
				Content   *string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return AssembledMessage{}, "", "", 0, nil, err
	}
	usage, err := parseOpenAIUsage(raw, models.TaskAPITypeChatCompletions)
	if err != nil {
		return AssembledMessage{}, "", "", 0, nil, err
	}
	msg := AssembledMessage{}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if choice.Message.Content != nil {
			msg.Content = *choice.Message.Content
		}
		msg.FinishReason = choice.FinishReason
		for _, tc := range choice.Message.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, AssembledToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
	}
	return msg, resp.ID, resp.Model, resp.Created, usage, nil
}
