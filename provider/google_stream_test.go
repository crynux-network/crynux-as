package provider

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
	"testing"
)

func TestGoogleStreamAssembly(t *testing.T) {
	body := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"text":"Hel"}]},"finishReason":""}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"totalTokenCount":3}}`,
		`data: {"candidates":[{"content":{"parts":[{"text":"Hello"}]},"finishReason":""}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":2,"totalTokenCount":4}}`,
		`data: {"candidates":[{"content":{"parts":[{"text":"Hello"},{"functionCall":{"id":"fc_1","name":"lookup","args":{"q":"x"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":5,"totalTokenCount":7}}`,
		"",
	}, "\n")

	adapter := &GoogleAdapter{}
	result, err := adapter.readStream(strings.NewReader(body), models.TaskAPITypeChatCompletions, "gemini-test", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result.Acceptance != AcceptanceAcceptedBillable {
		t.Fatalf("acceptance=%s msg=%s", result.Acceptance, result.ErrorMessage)
	}
	var parsed map[string]any
	if err := json.Unmarshal(result.FinalResultJSON, &parsed); err != nil {
		t.Fatalf("json: %v", err)
	}
	msg := parsed["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "Hello" {
		t.Fatalf("content=%v", msg["content"])
	}
	tc := msg["tool_calls"].([]any)[0].(map[string]any)
	if tc["id"] != "fc_1" {
		t.Fatalf("tool id=%v", tc["id"])
	}
	fn := tc["function"].(map[string]any)
	if fn["name"] != "lookup" {
		t.Fatalf("tool=%v", fn)
	}
}

func TestGoogleStreamParallelSameNameTools(t *testing.T) {
	body := strings.Join([]string{
		`data: {"candidates":[{"content":{"parts":[{"functionCall":{"id":"a","name":"lookup","args":{"q":"1"}}},{"functionCall":{"id":"b","name":"lookup","args":{"q":"2"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":2,"totalTokenCount":3}}`,
		"",
	}, "\n")
	adapter := &GoogleAdapter{}
	result, err := adapter.readStream(strings.NewReader(body), models.TaskAPITypeChatCompletions, "gemini-test", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(result.FinalResultJSON, &parsed)
	msg := parsed["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	tools := msg["tool_calls"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(tools), tools)
	}
	if tools[0].(map[string]any)["id"] != "a" || tools[1].(map[string]any)["id"] != "b" {
		t.Fatalf("tools=%v", tools)
	}
}
