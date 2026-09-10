package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestParseResponsesRequestRejectsUnsupportedFields(t *testing.T) {
	body := []byte(`{"model":"qwen/qwen3-7b","input":"hello","stream":true}`)
	_, err := ParseResponsesRequest(body)
	if err == nil {
		t.Fatal("expected error for stream=true")
	}
}

func TestParseResponsesRequestAcceptsPreviousResponseID(t *testing.T) {
	body := []byte(`{"model":"qwen/qwen3-7b","input":"hello","previous_response_id":"resp_old"}`)
	req, err := ParseResponsesRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.PreviousResponseID != "resp_old" {
		t.Fatalf("previous_response_id = %q", req.PreviousResponseID)
	}
}

func TestBuildResponsesTaskArgsFromStringInput(t *testing.T) {
	req, err := ParseResponsesRequest([]byte(`{
		"model":"qwen/qwen3-7b",
		"input":"hello",
		"instructions":"be helpful"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	taskArgsJSON, err := BuildResponsesTaskArgs(req, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "qwen/qwen3-7b" {
		t.Fatalf("unexpected model: %v", payload["model"])
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("expected system+user messages, got %v", payload["messages"])
	}
}

func TestParseResponsesRequestAcceptsEasyInputMessageWithoutType(t *testing.T) {
	req, err := ParseResponsesRequest([]byte(`{
		"model":"qwen/qwen3-7b",
		"input":[
			{"role":"system","content":"Summarize."},
			{"role":"user","content":"Hello world."}
		],
		"tools":[{
			"type":"function",
			"name":"LLMTextResult",
			"description":"",
			"parameters":{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}
		}],
		"tool_choice":{"type":"function","name":"LLMTextResult"},
		"background":true
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Input.Items) != 2 {
		t.Fatalf("expected 2 input items, got %d", len(req.Input.Items))
	}
	if req.Input.Items[0].Type != "" || req.Input.Items[0].Role != "system" {
		t.Fatalf("unexpected first item: %+v", req.Input.Items[0])
	}

	taskArgsJSON, err := BuildResponsesTaskArgs(req, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		t.Fatal(err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %v", payload["messages"])
	}
	first, ok := messages[0].(map[string]any)
	if !ok || first["role"] != "system" || first["content"] != "Summarize." {
		t.Fatalf("unexpected first message: %v", messages[0])
	}
	second, ok := messages[1].(map[string]any)
	if !ok || second["role"] != "user" || second["content"] != "Hello world." {
		t.Fatalf("unexpected second message: %v", messages[1])
	}
}

func TestParseResponsesRequestRejectsEmptyTypeWithoutRole(t *testing.T) {
	_, err := ParseResponsesRequest([]byte(`{
		"model":"qwen/qwen3-7b",
		"input":[{"content":"hello"}]
	}`))
	if err == nil {
		t.Fatal("expected error for input item without type and role")
	}
}

func TestBuildResponsesHistoryFromPreviousJob(t *testing.T) {
	taskArgs := `{"model":"qwen/qwen3-7b","messages":[{"role":"system","content":"old instructions"},{"role":"user","content":"hello"}],"seed":0}`
	raw := `{"model":"qwen/qwen3-7b","choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	history, err := BuildResponsesHistoryFromPreviousJob(taskArgs, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history len=%d", len(history))
	}
	if history[0].Role != models.LLMRoleUser || history[0].Content != "hello" {
		t.Fatalf("history[0]=%+v", history[0])
	}
	if history[1].Role != models.LLMRoleAssistant || history[1].Content != "hi there" {
		t.Fatalf("history[1]=%+v", history[1])
	}

	req, err := ParseResponsesRequest([]byte(`{
		"model":"qwen/qwen3-7b",
		"instructions":"new instructions",
		"input":"follow up"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	taskArgsJSON, err := BuildResponsesTaskArgsWithHistory(req, history, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgsJSON), &payload); err != nil {
		t.Fatal(err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("messages=%v", payload["messages"])
	}
	first := messages[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "hello" {
		t.Fatalf("first=%v", first)
	}
	third := messages[2].(map[string]any)
	if third["role"] != "system" || third["content"] != "new instructions" {
		t.Fatalf("third=%v", third)
	}
}

func TestBuildResponsesHistoryIncludesToolCalls(t *testing.T) {
	taskArgs := `{"model":"qwen/qwen3-7b","messages":[{"role":"user","content":"call tool"}],"seed":0}`
	raw := `{"model":"qwen/qwen3-7b","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"a\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	history, err := BuildResponsesHistoryFromPreviousJob(taskArgs, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history len=%d", len(history))
	}
	if len(history[1].ToolCalls) != 1 || history[1].ToolCalls[0].Function.Name != "lookup" {
		t.Fatalf("assistant=%+v", history[1])
	}
}

