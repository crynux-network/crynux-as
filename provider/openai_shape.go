package provider

import (
	"crynux_as/models"
	"encoding/json"
	"fmt"
	"time"
)

// AssembledMessage is the accumulated assistant output used for final public results.
type AssembledMessage struct {
	Content      string
	ToolCalls    []AssembledToolCall
	FinishReason string
}

type AssembledToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func BuildChatCompletionsResult(id, model string, created int64, msg AssembledMessage, usage *NormalizedUsage) ([]byte, error) {
	if created == 0 {
		created = time.Now().Unix()
	}
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%d", created)
	}
	message := map[string]any{
		"role":    "assistant",
		"content": msg.Content,
	}
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
		message["tool_calls"] = toolCalls
		if message["content"] == "" {
			message["content"] = nil
		}
	}
	finish := msg.FinishReason
	if finish == "" {
		if len(msg.ToolCalls) > 0 {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}
	out := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if usage != nil {
		out["usage"] = chatUsageMap(usage)
	}
	return json.Marshal(out)
}

func BuildResponsesResult(id, model string, msg AssembledMessage, usage *NormalizedUsage) ([]byte, error) {
	if id == "" {
		id = fmt.Sprintf("resp_%d", time.Now().UnixNano())
	}
	output := make([]map[string]any, 0, 1+len(msg.ToolCalls))
	if msg.Content != "" || len(msg.ToolCalls) == 0 {
		output = append(output, map[string]any{
			"type":    "message",
			"id":      id + "_msg",
			"role":    "assistant",
			"status":  "completed",
			"content": []map[string]any{{"type": "output_text", "text": msg.Content}},
		})
	}
	for i, tc := range msg.ToolCalls {
		output = append(output, map[string]any{
			"type":      "function_call",
			"id":        tc.ID,
			"call_id":   tc.ID,
			"name":      tc.Name,
			"arguments": tc.Arguments,
			"status":    "completed",
			"index":     responsesToolOutputIndex(i),
		})
	}
	status := "completed"
	out := map[string]any{
		"id":     id,
		"object": "response",
		"status": status,
		"model":  model,
		"output": output,
	}
	if usage != nil {
		out["usage"] = responsesUsageMap(usage)
	}
	return json.Marshal(out)
}

func BuildPublicResult(apiType models.TaskAPIType, id, model string, created int64, msg AssembledMessage, usage *NormalizedUsage) ([]byte, error) {
	switch apiType {
	case models.TaskAPITypeResponses:
		return BuildResponsesResult(id, model, msg, usage)
	default:
		return BuildChatCompletionsResult(id, model, created, msg, usage)
	}
}

func chatUsageMap(u *NormalizedUsage) map[string]any {
	m := map[string]any{
		"prompt_tokens":     u.InputTokens,
		"completion_tokens": u.OutputTokens,
		"total_tokens":      u.TotalTokens,
	}
	if u.CachedInputTokens > 0 {
		m["prompt_tokens_details"] = map[string]any{"cached_tokens": u.CachedInputTokens}
	}
	if u.ReasoningTokens > 0 {
		m["completion_tokens_details"] = map[string]any{"reasoning_tokens": u.ReasoningTokens}
	}
	return m
}

func responsesUsageMap(u *NormalizedUsage) map[string]any {
	m := map[string]any{
		"input_tokens":  u.InputTokens,
		"output_tokens": u.OutputTokens,
		"total_tokens":  u.TotalTokens,
	}
	if u.CachedInputTokens > 0 {
		m["input_tokens_details"] = map[string]any{"cached_tokens": u.CachedInputTokens}
	}
	if u.ReasoningTokens > 0 {
		m["output_tokens_details"] = map[string]any{"reasoning_tokens": u.ReasoningTokens}
	}
	return m
}

// responsesToolOutputIndex maps dense tool indexes onto public Responses
// output_index values. Text content always occupies output_index 0.
func responsesToolOutputIndex(toolIndex int) int {
	return 1 + toolIndex
}

func MapAnthropicStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}

func MapGoogleFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	default:
		if reason == "" {
			return "stop"
		}
		return "stop"
	}
}
