package llmadapter

import (
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

func TestParseResponsesRequestRejectsPreviousResponseID(t *testing.T) {
	body := []byte(`{"model":"qwen/qwen3-7b","input":"hello","previous_response_id":"resp_old"}`)
	_, err := ParseResponsesRequest(body)
	if err == nil {
		t.Fatal("expected error for previous_response_id")
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

func TestFormatResponsesPendingObject(t *testing.T) {
	payload, err := FormatResponsesPendingObject(ResponsesObjectParams{
		ID:         "resp_test",
		Model:      "qwen/qwen3-7b",
		CreatedAt:  100,
		Status:     ResponsesStatusQueued,
		Background: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		t.Fatal(err)
	}
	if obj["status"] != ResponsesStatusQueued {
		t.Fatalf("status=%v", obj["status"])
	}
	if obj["object"] != "response" {
		t.Fatalf("object=%v", obj["object"])
	}
}
