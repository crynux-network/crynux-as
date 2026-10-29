package provider

import (
	"crynux_as/models"
	"encoding/json"
	"strings"
	"testing"
)

func TestGoogleRequestConversionTools(t *testing.T) {
	reqJSON := []byte(`{
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"user","content":"hi"},
			{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"a\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"ok"}
		],
		"tools":[{"type":"function","function":{"name":"lookup","description":"d","parameters":{"type":"object"}}}],
		"tool_choice":{"type":"function","function":{"name":"lookup"}},
		"stop":["END"],
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
	names := cfg["allowedFunctionNames"].([]any)
	if names[0] != "lookup" {
		t.Fatalf("allowed=%v", names)
	}
	gen := out["generationConfig"].(map[string]any)
	seqs := gen["stopSequences"].([]any)
	if seqs[0] != "END" {
		t.Fatalf("stopSequences=%v", seqs)
	}
	contents := out["contents"].([]any)
	modelMsg := contents[1].(map[string]any)
	if modelMsg["role"] != "model" {
		t.Fatalf("model role=%v", modelMsg["role"])
	}
	fc := modelMsg["parts"].([]any)[0].(map[string]any)["functionCall"].(map[string]any)
	if fc["name"] != "lookup" || fc["id"] != "call_1" {
		t.Fatalf("functionCall=%v", fc)
	}
	toolMsg := contents[2].(map[string]any)
	fr := toolMsg["parts"].([]any)[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "lookup" || fr["id"] != "call_1" {
		t.Fatalf("functionResponse=%v", fr)
	}
}

func TestGoogleNamedToolChoiceResponses(t *testing.T) {
	reqJSON := []byte(`{
		"input":"x",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"lookup"}
	}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "m", models.TaskAPITypeResponses)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	native, err := convertCanonicalToGoogle(canonical)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(native, &out)
	cfg := out["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
	names := cfg["allowedFunctionNames"].([]any)
	if cfg["mode"] != "ANY" || names[0] != "lookup" {
		t.Fatalf("toolConfig=%v", cfg)
	}
}

func TestGoogleToolsAndStructuredOutputRejected(t *testing.T) {
	reqJSON := []byte(`{
		"messages":[{"role":"user","content":"x"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"response_format":{"type":"json_object"}
	}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "m", models.TaskAPITypeChatCompletions)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = convertCanonicalToGoogle(canonical)
	if err == nil || !strings.Contains(err.Error(), "does not support tools together") {
		t.Fatalf("expected mutex error, got %v", err)
	}
}

func TestGoogleJSONSchemaWithoutTools(t *testing.T) {
	reqJSON := []byte(`{
		"messages":[{"role":"user","content":"x"}],
		"response_format":{"type":"json_schema","json_schema":{"name":"out","schema":{"type":"object","properties":{"a":{"type":"string"}}}}}
	}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "m", models.TaskAPITypeChatCompletions)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	native, err := convertCanonicalToGoogle(canonical)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var out map[string]any
	_ = json.Unmarshal(native, &out)
	gen := out["generationConfig"].(map[string]any)
	if gen["responseMimeType"] != "application/json" {
		t.Fatalf("gen=%v", gen)
	}
	schema := gen["responseSchema"].(map[string]any)
	if schema["type"] != "object" {
		t.Fatalf("schema=%v", schema)
	}
}

func TestGoogleResponseFunctionCall(t *testing.T) {
	raw := []byte(`{
		"candidates":[{
			"content":{"parts":[
				{"text":"ok"},
				{"functionCall":{"id":"fc_1","name":"lookup","args":{"q":"a"}}}
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
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "lookup" || msg.ToolCalls[0].ID != "fc_1" || msg.ToolCalls[0].Arguments != `{"q":"a"}` {
		t.Fatalf("tools=%v", msg.ToolCalls)
	}
	if usage.ReasoningTokens != 1 || usage.TotalTokens != 8 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestGoogleResponseFunctionCallSyntheticID(t *testing.T) {
	raw := []byte(`{
		"candidates":[{
			"content":{"parts":[
				{"text":"ok"},
				{"functionCall":{"name":"lookup","args":{}}}
			]},
			"finishReason":"STOP"
		}],
		"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}
	}`)
	msg, _, err := parseGoogleGenerateContentResponse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.ToolCalls[0].ID != "call_google_0" {
		t.Fatalf("id=%q", msg.ToolCalls[0].ID)
	}
}
