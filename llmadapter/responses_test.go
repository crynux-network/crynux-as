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
