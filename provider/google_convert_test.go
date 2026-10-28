package provider

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestGoogleRequestConversionToolsAndSchema(t *testing.T) {
	reqJSON := []byte(`{
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"user","content":"hi"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"a\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"ok"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","description":"d","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"lookup"}},
		"response_format":{"type":"json_object"},
		"max_tokens":128
	}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "gemini-test", models.TaskAPITypeChatCompletions)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	native, err := convertCanonicalToGoogle(canonical)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(native, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sys := out["systemInstruction"].(map[string]any)
	parts := sys["parts"].([]any)
	if parts[0].(map[string]any)["text"] != "sys" {
		t.Fatalf("system=%v", sys)
	}
	tools := out["tools"].([]any)[0].(map[string]any)
	decls := tools["functionDeclarations"].([]any)
	if decls[0].(map[string]any)["name"] != "lookup" {
		t.Fatalf("decls=%v", decls)
	}
	cfg := out["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
	if cfg["mode"] != "ANY" {
		t.Fatalf("toolConfig=%v", cfg)
	}
	gen := out["generationConfig"].(map[string]any)
	if gen["responseMimeType"] != "application/json" {
		t.Fatalf("gen=%v", gen)
	}
	contents := out["contents"].([]any)
	modelMsg := contents[1].(map[string]any)
	if modelMsg["role"] != "model" {
		t.Fatalf("model role=%v", modelMsg["role"])
	}
	fc := modelMsg["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "lookup" {
		t.Fatalf("functionCall=%v", fc)
	}
	toolMsg := contents[2].(map[string]any)
	fr := toolMsg["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "lookup" {
		t.Fatalf("functionResponse=%v", fr)
	}
}

func TestGoogleResponseFunctionCall(t *testing.T) {
	raw := []byte(`{
		"candidates":[{
			"content":{"parts":[
				{"text":"ok"},
				{"functionCall":{"name":"lookup","args":{"q":"a"}}}
			]},
			"finishReason":"STOP"
		}],
		"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8,"thoughtsTokenCount":1}
	}`)
	msg, usage, err := parseGoogleGenerateContentResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Content != "ok" || msg.FinishReason != "tool_calls" {
		t.Fatalf("msg=%+v", msg)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "lookup" || msg.ToolCalls[0].Arguments != `{"q":"a"}` {
		t.Fatalf("tools=%v", msg.ToolCalls)
	}
	if usage.ReasoningTokens != 1 || usage.TotalTokens != 8 {
		t.Fatalf("usage=%+v", usage)
	}
}
