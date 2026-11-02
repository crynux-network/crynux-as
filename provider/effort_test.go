package provider

import (
	"crynux_as/models"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"crynux_as/llmadapter"
)

func TestParseCanonicalReasoningEffortChatAndResponses(t *testing.T) {
	chatJSON := []byte(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"xhigh"
	}`)
	chat, err := ParseCanonicalRequest(chatJSON, "gpt-test", models.TaskAPITypeChatCompletions, 128)
	if err != nil {
		t.Fatal(err)
	}
	if chat.ReasoningEffort != "xhigh" {
		t.Fatalf("chat effort=%q", chat.ReasoningEffort)
	}

	respJSON := []byte(`{
		"model":"gpt-test",
		"input":"hi",
		"reasoning":{"effort":"minimal"}
	}`)
	resp, err := ParseCanonicalRequest(respJSON, "gpt-test", models.TaskAPITypeResponses, 128)
	if err != nil {
		t.Fatal(err)
	}
	if resp.ReasoningEffort != "minimal" {
		t.Fatalf("responses effort=%q", resp.ReasoningEffort)
	}
}

func TestParseCanonicalReasoningEffortRejectsInvalid(t *testing.T) {
	_, err := ParseCanonicalRequest([]byte(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"ultra"
	}`), "gpt-test", models.TaskAPITypeChatCompletions, 128)
	var validationErr *llmadapter.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
}

func TestOpenAIConversionPassesReasoningEffort(t *testing.T) {
	for _, effort := range llmadapter.AllowedReasoningEffortValues {
		req := CanonicalRequest{Model: "gpt-test", ReasoningEffort: effort, MaxTokens: 16}
		body, err := convertCanonicalToOpenAIChat(req)
		if err != nil {
			t.Fatalf("%s: %v", effort, err)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		if out["reasoning_effort"] != effort {
			t.Fatalf("%s: body=%s", effort, body)
		}
	}
	body, err := convertCanonicalToOpenAIChat(CanonicalRequest{Model: "gpt-test", MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "reasoning_effort") {
		t.Fatalf("omitted effort still present: %s", body)
	}
}

func TestAnthropicConversionMapsReasoningEffort(t *testing.T) {
	noneBody, err := convertCanonicalToAnthropic(CanonicalRequest{Model: "claude-test", ReasoningEffort: "none", MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	var noneOut map[string]any
	if err := json.Unmarshal(noneBody, &noneOut); err != nil {
		t.Fatal(err)
	}
	thinking, _ := noneOut["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Fatalf("none thinking=%v", noneOut["thinking"])
	}
	if _, ok := noneOut["output_config"]; ok {
		t.Fatalf("none should omit output_config: %s", noneBody)
	}

	cases := map[string]string{
		"minimal": "low",
		"low":     "low",
		"medium":  "medium",
		"high":    "high",
		"xhigh":   "xhigh",
		"max":     "max",
	}
	for in, want := range cases {
		body, err := convertCanonicalToAnthropic(CanonicalRequest{
			Model:           "claude-test",
			ReasoningEffort: in,
			MaxTokens:       16,
			ResponseFormat:  map[string]any{"type": "json_object"},
		})
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		thinking, _ := out["thinking"].(map[string]any)
		if thinking["type"] != "adaptive" {
			t.Fatalf("%s thinking=%v", in, out["thinking"])
		}
		cfg, _ := out["output_config"].(map[string]any)
		if cfg["effort"] != want {
			t.Fatalf("%s effort=%v body=%s", in, cfg["effort"], body)
		}
		if cfg["format"] == nil {
			t.Fatalf("%s missing merged format: %s", in, body)
		}
	}

	omitted, err := convertCanonicalToAnthropic(CanonicalRequest{Model: "claude-test", MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(omitted), `"thinking"`) || strings.Contains(string(omitted), `"effort"`) {
		t.Fatalf("omitted effort still present: %s", omitted)
	}
}

func TestGoogleConversionMapsReasoningEffort(t *testing.T) {
	noneBody, err := convertCanonicalToGoogle(CanonicalRequest{Model: "gemini-test", ReasoningEffort: "none", MaxTokens: 16})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(noneBody), "thinkingConfig") {
		t.Fatalf("none should omit thinkingConfig: %s", noneBody)
	}

	cases := map[string]string{
		"minimal": "low",
		"low":     "low",
		"medium":  "medium",
		"high":    "high",
		"xhigh":   "high",
		"max":     "high",
	}
	for in, want := range cases {
		body, err := convertCanonicalToGoogle(CanonicalRequest{Model: "gemini-test", ReasoningEffort: in, MaxTokens: 16})
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		var out map[string]any
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatal(err)
		}
		gen, _ := out["generationConfig"].(map[string]any)
		cfg, _ := gen["thinkingConfig"].(map[string]any)
		if cfg["thinkingLevel"] != want {
			t.Fatalf("%s level=%v body=%s", in, cfg["thinkingLevel"], body)
		}
	}
}

func TestGoogleUsageMarksReasoningOutsideOutput(t *testing.T) {
	raw := []byte(`{
		"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":9,"thoughtsTokenCount":1}
	}`)
	_, usage, err := parseGoogleGenerateContentResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if usage.ReasoningIncludedInOutput {
		t.Fatal("google thoughts must not be marked as included in output")
	}
	if usage.ReasoningTokens != 1 || usage.OutputTokens != 3 {
		t.Fatalf("usage=%+v", usage)
	}
}

func TestOpenAIUsageMarksReasoningIncludedInOutput(t *testing.T) {
	raw := []byte(`{
		"usage":{
			"prompt_tokens":2,
			"completion_tokens":10,
			"total_tokens":12,
			"completion_tokens_details":{"reasoning_tokens":4}
		}
	}`)
	usage, err := parseOpenAIUsage(raw, models.TaskAPITypeChatCompletions)
	if err != nil {
		t.Fatal(err)
	}
	if !usage.ReasoningIncludedInOutput {
		t.Fatal("openai reasoning must be marked as included in output")
	}
	if usage.ReasoningTokens != 4 || usage.OutputTokens != 10 {
		t.Fatalf("usage=%+v", usage)
	}
}
