package provider

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestOmittedMaxTokensUsesConfiguredDefault(t *testing.T) {
	const defaultMax = 2048
	chatJSON := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	responsesJSON := []byte(`{"input":"hi"}`)

	chatCanon, err := ParseCanonicalRequest(chatJSON, "m", models.TaskAPITypeChatCompletions, defaultMax)
	if err != nil {
		t.Fatalf("chat parse: %v", err)
	}
	if chatCanon.MaxTokens != defaultMax {
		t.Fatalf("chat MaxTokens=%d want %d", chatCanon.MaxTokens, defaultMax)
	}

	respCanon, err := ParseCanonicalRequest(responsesJSON, "m", models.TaskAPITypeResponses, defaultMax)
	if err != nil {
		t.Fatalf("responses parse: %v", err)
	}
	if respCanon.MaxTokens != defaultMax {
		t.Fatalf("responses MaxTokens=%d want %d", respCanon.MaxTokens, defaultMax)
	}

	openaiBody, err := convertCanonicalToOpenAIChat(chatCanon)
	if err != nil {
		t.Fatalf("openai convert: %v", err)
	}
	var openaiOut map[string]any
	if err := json.Unmarshal(openaiBody, &openaiOut); err != nil {
		t.Fatalf("openai unmarshal: %v", err)
	}
	if openaiOut["max_completion_tokens"].(float64) != float64(defaultMax) {
		t.Fatalf("openai max_completion_tokens=%v", openaiOut["max_completion_tokens"])
	}

	anthropicBody, err := convertCanonicalToAnthropic(chatCanon)
	if err != nil {
		t.Fatalf("anthropic convert: %v", err)
	}
	var anthropicOut map[string]any
	if err := json.Unmarshal(anthropicBody, &anthropicOut); err != nil {
		t.Fatalf("anthropic unmarshal: %v", err)
	}
	if anthropicOut["max_tokens"].(float64) != float64(defaultMax) {
		t.Fatalf("anthropic max_tokens=%v", anthropicOut["max_tokens"])
	}

	googleBody, err := convertCanonicalToGoogle(chatCanon)
	if err != nil {
		t.Fatalf("google convert: %v", err)
	}
	var googleOut map[string]any
	if err := json.Unmarshal(googleBody, &googleOut); err != nil {
		t.Fatalf("google unmarshal: %v", err)
	}
	gen := googleOut["generationConfig"].(map[string]any)
	if gen["maxOutputTokens"].(float64) != float64(defaultMax) {
		t.Fatalf("google maxOutputTokens=%v", gen["maxOutputTokens"])
	}
}

func TestExplicitMaxTokensOverridesDefault(t *testing.T) {
	reqJSON := []byte(`{"messages":[{"role":"user","content":"hi"}],"max_completion_tokens":64}`)
	canonical, err := ParseCanonicalRequest(reqJSON, "m", models.TaskAPITypeChatCompletions, 2048)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if canonical.MaxTokens != 64 {
		t.Fatalf("MaxTokens=%d want 64", canonical.MaxTokens)
	}
}
