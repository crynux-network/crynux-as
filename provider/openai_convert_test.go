package provider

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConvertCanonicalToOpenAIChatFromResponses(t *testing.T) {
	reqJSON := []byte(`{
		"model":"gpt-test",
		"instructions":"be brief",
		"input":"hello",
		"previous_response_id":"resp_should_not_appear",
		"max_output_tokens":32,
		"tools":[{"type":"function","name":"lookup","description":"d","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"lookup"}
	}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "gpt-test", models.TaskAPITypeResponses)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	native, err := convertCanonicalToOpenAIChat(canonical)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(native, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := out["previous_response_id"]; ok {
		t.Fatalf("previous_response_id leaked: %v", out)
	}
	if out["max_completion_tokens"].(float64) != 32 {
		t.Fatalf("max_completion_tokens=%v", out["max_completion_tokens"])
	}
	messages := out["messages"].([]any)
	if len(messages) < 2 {
		t.Fatalf("messages=%v", messages)
	}
	if messages[0].(map[string]any)["role"] != "system" {
		t.Fatalf("first message=%v", messages[0])
	}
	choice := out["tool_choice"].(map[string]any)
	fn := choice["function"].(map[string]any)
	if choice["type"] != "function" || fn["name"] != "lookup" {
		t.Fatalf("tool_choice=%v", choice)
	}
}

func TestOpenAIExecuteResponsesUsesChatCompletions(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("x-request-id", "req-1")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-upstream",
			"object":"chat.completion",
			"created":1,
			"model":"gpt-test",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}
		}`))
	}))
	defer server.Close()

	adapter := NewOpenAIAdapter(config.ProviderServiceConfig{
		BaseURL:                            server.URL,
		RequestTimeoutSeconds:              5,
		ResponseHeaderTimeoutSeconds:       5,
		CredentialValidationTimeoutSeconds: 5,
	})
	result, err := adapter.Execute(
		context.Background(),
		[]byte("sk-test"),
		models.TaskAPITypeResponses,
		"gpt-test",
		[]byte(`{"model":"gpt-test","input":"hello","previous_response_id":"resp_old"}`),
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("execute err: %v", err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path=%q", gotPath)
	}
	if _, ok := gotBody["previous_response_id"]; ok {
		t.Fatalf("upstream body leaked previous_response_id: %v", gotBody)
	}
	if result.Acceptance != AcceptanceAcceptedBillable {
		t.Fatalf("acceptance=%s msg=%s", result.Acceptance, result.ErrorMessage)
	}
	var parsed map[string]any
	if err := json.Unmarshal(result.FinalResultJSON, &parsed); err != nil {
		t.Fatalf("result: %v", err)
	}
	if parsed["object"] != "response" {
		t.Fatalf("object=%v", parsed["object"])
	}
	if !strings.Contains(string(result.FinalResultJSON), `"hi"`) {
		t.Fatalf("result=%s", result.FinalResultJSON)
	}
}

func TestRewritePublicResultID(t *testing.T) {
	in := []byte(`{"id":"chatcmpl-x","object":"response","status":"completed"}`)
	out, err := RewritePublicResultID(in, "resp_public")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["id"] != "resp_public" {
		t.Fatalf("id=%v", parsed["id"])
	}
}
