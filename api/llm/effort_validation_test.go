package llm

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"testing"
)

func TestChatCompletionsParseRejectsInvalidReasoningEffortBeforeCredits(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6-astra",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"ultra"
	}`)
	_, err := parseTaskJobRequest(body, models.TaskAPITypeChatCompletions, 128)
	var validationErr *llmadapter.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if validationErr.Field != "reasoning_effort" {
		t.Fatalf("field=%q", validationErr.Field)
	}
}

func TestProviderResponsesEstimateRejectsInvalidReasoningEffortBeforeCredits(t *testing.T) {
	body := []byte(`{
		"model":"gpt-6-astra",
		"input":"hi",
		"reasoning":{"effort":"ultra"}
	}`)
	parsed, err := parseProviderResponsesRequest(body, "gpt-6-astra")
	if err != nil {
		t.Fatalf("lightweight responses parse should accept body shape: %v", err)
	}
	if parsed.model != "gpt-6-astra" {
		t.Fatalf("model=%q", parsed.model)
	}
	route := service.ProviderModelRoute{
		ServiceProvider: models.ServiceProviderOpenAI,
		ModelName:       "gpt-6-astra",
	}
	_, err = service.EstimateProviderInputTokens(route, models.TaskAPITypeResponses, body, 1<<20)
	if err == nil {
		t.Fatal("expected invalid reasoning.effort rejection before credits reservation")
	}
}
