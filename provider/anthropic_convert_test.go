package provider

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestAnthropicRequestConversionToolsAndSystem(t *testing.T) {
	reqJSON := []byte(`{
		"model":"ignored",
		"messages":[
			{"role":"system","content":"be helpful"},
			{"role":"user","content":"weather?"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"SF\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"sunny"}
		],
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],
		"tool_choice":"auto",
		"response_format":{"type":"json_schema","json_schema":{"name":"out","schema":{"type":"object"},"strict":true}},
		"max_tokens":200
	}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "claude-test", models.TaskAPITypeChatCompletions)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	native, err := convertCanonicalToAnthropic(canonical)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(native, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["system"] != "be helpful" {
		t.Fatalf("system=%v", out["system"])
	}
	tools := out["tools"].([]any)
	tool0 := tools[0].(map[string]any)
	if tool0["name"] != "get_weather" {
		t.Fatalf("tool=%v", tool0)
	}
	if _, ok := tool0["input_schema"]; !ok {
		t.Fatalf("missing input_schema: %v", tool0)
	}
	if _, ok := tool0["function"]; ok {
		t.Fatalf("should not nest function: %v", tool0)
	}
	choice := out["tool_choice"].(map[string]any)
	if choice["type"] != "auto" {
		t.Fatalf("tool_choice=%v", choice)
	}
	format := out["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("format=%v", format)
	}
	if _, ok := format["name"]; ok {
		t.Fatalf("format should not include name: %v", format)
	}
	if _, ok := format["strict"]; ok {
		t.Fatalf("format should not include strict: %v", format)
	}
	if _, ok := format["schema"]; !ok {
		t.Fatalf("format missing schema: %v", format)
	}
	messages := out["messages"].([]any)
	assistant := messages[1].(map[string]any)
	content := assistant["content"].([]any)
	foundToolUse := false
	for _, part := range content {
		p := part.(map[string]any)
		if p["type"] == "tool_use" {
			foundToolUse = true
			if p["id"] != "call_1" || p["name"] != "get_weather" {
				t.Fatalf("tool_use=%v", p)
			}
		}
	}
	if !foundToolUse {
		t.Fatalf("missing tool_use in %v", content)
	}
	toolMsg := messages[2].(map[string]any)
	if toolMsg["role"] != "user" {
		t.Fatalf("tool result role=%v", toolMsg["role"])
	}
	toolContent := toolMsg["content"].([]any)
	tr := toolContent[0].(map[string]any)
	if tr["type"] != "tool_result" || tr["tool_use_id"] != "call_1" {
		t.Fatalf("tool_result=%v", tr)
	}
}

func TestAnthropicNamedToolChoiceChatAndResponses(t *testing.T) {
	chatJSON := []byte(`{
		"messages":[{"role":"user","content":"x"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"lookup"}}
	}`)
	chatCanon, err := ParseCanonicalRequest(chatJSON, "m", models.TaskAPITypeChatCompletions)
	if err != nil {
		t.Fatalf("chat parse: %v", err)
	}
	chatNative, err := convertCanonicalToAnthropic(chatCanon)
	if err != nil {
		t.Fatalf("chat convert: %v", err)
	}
	var chatOut map[string]any
	_ = json.Unmarshal(chatNative, &chatOut)
	chatChoice := chatOut["tool_choice"].(map[string]any)
	if chatChoice["type"] != "tool" || chatChoice["name"] != "lookup" {
		t.Fatalf("chat tool_choice=%v", chatChoice)
	}

	respJSON := []byte(`{
		"input":"x",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"lookup"}
	}`)
	respCanon, err := ParseCanonicalRequest(respJSON, "m", models.TaskAPITypeResponses)
	if err != nil {
		t.Fatalf("responses parse: %v", err)
	}
	respNative, err := convertCanonicalToAnthropic(respCanon)
	if err != nil {
		t.Fatalf("responses convert: %v", err)
	}
	var respOut map[string]any
	_ = json.Unmarshal(respNative, &respOut)
	respChoice := respOut["tool_choice"].(map[string]any)
	if respChoice["type"] != "tool" || respChoice["name"] != "lookup" {
		t.Fatalf("responses tool_choice=%v", respChoice)
	}
}

func TestAnthropicJSONObjectAndParallelAndNone(t *testing.T) {
	reqJSON := []byte(`{
		"messages":[{"role":"user","content":"x"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"tool_choice":"none",
		"parallel_tool_calls":false,
		"response_format":{"type":"json_object"}
	}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "m", models.TaskAPITypeChatCompletions)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	native, err := convertCanonicalToAnthropic(canonical)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(native, &out)
	if _, ok := out["tools"]; !ok {
		t.Fatalf("tools should be kept with tool_choice none: %v", out)
	}
	choice := out["tool_choice"].(map[string]any)
	if choice["type"] != "none" {
		t.Fatalf("tool_choice=%v", choice)
	}
	format := out["output_config"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("format=%v", format)
	}
	schema := format["schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("schema=%v", schema)
	}

	autoJSON := []byte(`{
		"messages":[{"role":"user","content":"x"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"parallel_tool_calls":false
	}`)
	autoCanon, _ := ParseCanonicalRequest(autoJSON, "m", models.TaskAPITypeChatCompletions)
	autoNative, err := convertCanonicalToAnthropic(autoCanon)
	if err != nil {
		t.Fatalf("auto convert: %v", err)
	}
	var autoOut map[string]any
	_ = json.Unmarshal(autoNative, &autoOut)
	autoChoice := autoOut["tool_choice"].(map[string]any)
	if autoChoice["type"] != "auto" || autoChoice["disable_parallel_tool_use"] != true {
		t.Fatalf("auto tool_choice=%v", autoChoice)
	}
}

func TestAnthropicResponseToolUse(t *testing.T) {
	raw := []byte(`{
		"content":[
			{"type":"text","text":"calling"},
			{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"SF"}}
		],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}
	}`)
	msg, usage, err := parseAnthropicMessageResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Content != "calling" || msg.FinishReason != "tool_calls" {
		t.Fatalf("msg=%+v", msg)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("tools=%v", msg.ToolCalls)
	}
	if usage.CachedInputTokens != 2 || usage.CacheWriteInputTokens != 1 {
		t.Fatalf("usage=%+v", usage)
	}
	public, err := BuildChatCompletionsResult("id", "m", 1, msg, usage)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(public, &out)
	choice := out["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish=%v", choice["finish_reason"])
	}
}
