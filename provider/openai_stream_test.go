package provider

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAIChatStreamAccumulatorAssembly(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"lookup","arguments":"{\"q\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}}`,
		`data: [DONE]`,
		"",
	}, "\n")

	var kinds []StreamEventKind
	sink := func(e StreamEvent) { kinds = append(kinds, e.Kind) }
	adapter := &OpenAIAdapter{}
	result, err := adapter.readChatStream(strings.NewReader(body), models.TaskAPITypeChatCompletions, "gpt-test", "req-1", sink)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result.Acceptance != AcceptanceAcceptedBillable {
		t.Fatalf("acceptance=%s msg=%s", result.Acceptance, result.ErrorMessage)
	}
	if result.FinalUsage == nil || result.FinalUsage.TotalTokens != 14 {
		t.Fatalf("usage=%v", result.FinalUsage)
	}
	var parsed map[string]any
	if err := json.Unmarshal(result.FinalResultJSON, &parsed); err != nil {
		t.Fatalf("result json: %v", err)
	}
	choices := parsed["choices"].([]any)
	msg := choices[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "Hello" {
		t.Fatalf("content=%v", msg["content"])
	}
	toolCalls := msg["tool_calls"].([]any)
	fn := toolCalls[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "lookup" || fn["arguments"] != `{"q":"x"}` {
		t.Fatalf("tool=%v", fn)
	}
	startCount := 0
	for _, k := range kinds {
		if k == StreamEventToolCallStart {
			startCount++
		}
	}
	if startCount != 1 {
		t.Fatalf("expected one ToolCallStart, got %d kinds=%v", startCount, kinds)
	}
	if !containsKind(kinds, StreamEventContentDelta) || !containsKind(kinds, StreamEventFinalUsage) {
		t.Fatalf("kinds=%v", kinds)
	}
}

func TestOpenAIResponsesUsesChatStreamAssembly(t *testing.T) {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":1,"model":"gpt-test","choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
		`data: [DONE]`,
		"",
	}, "\n")
	var finish string
	sink := func(e StreamEvent) {
		if e.Kind == StreamEventFinish {
			var p FinishPayload
			_ = json.Unmarshal(e.Payload, &p)
			finish = p.FinishReason
		}
	}
	adapter := &OpenAIAdapter{}
	result, err := adapter.readChatStream(strings.NewReader(body), models.TaskAPITypeResponses, "gpt-test", "req-1", sink)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result.Acceptance != AcceptanceAcceptedBillable {
		t.Fatalf("acceptance=%s msg=%s", result.Acceptance, result.ErrorMessage)
	}
	if finish != "length" {
		t.Fatalf("finish=%q", finish)
	}
	var parsed map[string]any
	if err := json.Unmarshal(result.FinalResultJSON, &parsed); err != nil {
		t.Fatalf("result json: %v", err)
	}
	if parsed["object"] != "response" {
		t.Fatalf("object=%v", parsed["object"])
	}
	if parsed["id"] != "chatcmpl-x" {
		t.Fatalf("id=%v", parsed["id"])
	}
}

func containsKind(kinds []StreamEventKind, want StreamEventKind) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}
