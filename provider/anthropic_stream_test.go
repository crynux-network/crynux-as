package provider

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
	"testing"
)

func TestAnthropicStreamSemanticEvents(t *testing.T) {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":9,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"x\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":1}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var kinds []StreamEventKind
	sink := func(e StreamEvent) { kinds = append(kinds, e.Kind) }
	adapter := &AnthropicAdapter{}
	result, err := adapter.readStream(strings.NewReader(body), models.TaskAPITypeChatCompletions, "claude-test", "rid", sink)
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
	if msg["content"] != "Hi" {
		t.Fatalf("content=%v", msg["content"])
	}
	fn := msg["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if fn["arguments"] != `{"q":"x"}` {
		t.Fatalf("args=%v", fn["arguments"])
	}
	if !containsKind(kinds, StreamEventToolCallArgsDelta) || !containsKind(kinds, StreamEventFinish) {
		t.Fatalf("kinds=%v", kinds)
	}
}
