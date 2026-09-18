package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestBuildChatCompletionsTaskArgsStructuredFields(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3",
		"messages":[{"role":"user","content":"weather"}],
		"tools":[{"type":"function","function":{"name":"weather","parameters":{"type":"object"},"strict":true}}],
		"tool_choice":{"type":"function","function":{"name":"weather"}},
		"response_format":{"type":"json_schema","json_schema":{"name":"answer","schema":{"type":"object"}}}
	}`)
	taskArgsJSON, _, err := BuildChatCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	var args models.GPTTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if namedToolChoiceName(args.ToolChoice) != "weather" {
		t.Fatalf("tool_choice=%v", args.ToolChoice)
	}
	if args.ResponseFormat["type"] != "json_schema" {
		t.Fatalf("response_format=%v", args.ResponseFormat)
	}
}

func TestStructuredOutputsIsRejectedWhenPresent(t *testing.T) {
	chatBody := []byte(`{"model":"m","messages":[{"role":"user","content":"x"}],"structured_outputs":false}`)
	if _, _, err := BuildChatCompletionsTaskArgs(chatBody, 128); err == nil {
		t.Fatal("expected chat structured_outputs rejection")
	}
	responsesBody := []byte(`{"model":"m","input":"x","structured_outputs":null}`)
	if _, err := ParseResponsesRequest(responsesBody); err == nil {
		t.Fatal("expected responses structured_outputs rejection")
	}
}

func TestResponsesTextFormatBecomesCanonicalResponseFormat(t *testing.T) {
	req, err := ParseResponsesRequest([]byte(`{
		"model":"m",
		"input":"x",
		"text":{"format":{"type":"json_schema","name":"answer","schema":{"type":"object"},"strict":true}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.ResponseFormat["type"] != "json_schema" {
		t.Fatalf("response_format=%v", req.ResponseFormat)
	}
	definition := req.ResponseFormat["json_schema"].(map[string]interface{})
	if definition["name"] != "answer" {
		t.Fatalf("json_schema=%v", definition)
	}
}

func TestBuildChatCompletionsTaskArgsTemplateArgsFromKwargs(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"messages":[{"role":"user","content":"hi"}],
		"chat_template_kwargs":{"enable_thinking":false}
	}`)
	taskArgsJSON, _, err := BuildChatCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	var args models.GPTTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if args.TemplateArgs == nil || args.TemplateArgs["enable_thinking"] != false {
		t.Fatalf("template_args=%v", args.TemplateArgs)
	}
}

func TestBuildChatCompletionsTaskArgsTemplateArgsFromReasoningEffortNone(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"none"
	}`)
	taskArgsJSON, _, err := BuildChatCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	var args models.GPTTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if args.TemplateArgs == nil || args.TemplateArgs["enable_thinking"] != false {
		t.Fatalf("template_args=%v", args.TemplateArgs)
	}
}

func TestBuildChatCompletionsTaskArgsTemplateArgsFromReasoningEffortMedium(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"medium"
	}`)
	taskArgsJSON, _, err := BuildChatCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	var args models.GPTTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if args.TemplateArgs == nil || args.TemplateArgs["enable_thinking"] != true {
		t.Fatalf("template_args=%v", args.TemplateArgs)
	}
}

func TestBuildChatCompletionsTaskArgsKwargsEnableThinkingOverridesReasoningEffort(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"messages":[{"role":"user","content":"hi"}],
		"chat_template_kwargs":{"enable_thinking":false},
		"reasoning_effort":"high"
	}`)
	taskArgsJSON, _, err := BuildChatCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	var args models.GPTTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if args.TemplateArgs == nil || args.TemplateArgs["enable_thinking"] != false {
		t.Fatalf("template_args=%v", args.TemplateArgs)
	}
}

func TestBuildChatCompletionsTaskArgsOmitsTemplateArgsWhenUnset(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"messages":[{"role":"user","content":"hi"}]
	}`)
	taskArgsJSON, _, err := BuildChatCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["template_args"]; exists {
		t.Fatalf("expected template_args omitted, got %v", payload["template_args"])
	}
}
