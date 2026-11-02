package llmadapter

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateReasoningEffortAcceptsAllowedValues(t *testing.T) {
	for _, value := range AllowedReasoningEffortValues {
		got, err := ValidateReasoningEffort("reasoning_effort", value)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if got != value {
			t.Fatalf("%q: got %q", value, got)
		}
	}
	got, err := ValidateReasoningEffort("reasoning_effort", "")
	if err != nil || got != "" {
		t.Fatalf("empty: got=%q err=%v", got, err)
	}
}

func TestValidateReasoningEffortRejectsUnknown(t *testing.T) {
	_, err := ValidateReasoningEffort("reasoning_effort", "ultra")
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if validationErr.Field != "reasoning_effort" {
		t.Fatalf("field=%q", validationErr.Field)
	}
}

func TestBuildChatCompletionsTaskArgsRejectsInvalidReasoningEffort(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"messages":[{"role":"user","content":"hi"}],
		"reasoning_effort":"ultra"
	}`)
	_, _, err := BuildChatCompletionsTaskArgs(body, 128)
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if validationErr.Field != "reasoning_effort" {
		t.Fatalf("field=%q", validationErr.Field)
	}
}

func TestParseResponsesRequestRejectsInvalidReasoningEffort(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"input":"hi",
		"reasoning":{"effort":"ultra"}
	}`)
	_, err := ParseResponsesRequest(body)
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	if validationErr.Field != "reasoning.effort" {
		t.Fatalf("field=%q", validationErr.Field)
	}
}

func TestParseResponsesRequestNormalizesReasoningEffort(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"input":"hi",
		"reasoning":{"effort":" high "}
	}`)
	req, err := ParseResponsesRequest(body)
	if err != nil {
		t.Fatal(err)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "high" {
		raw, _ := json.Marshal(req.Reasoning)
		t.Fatalf("reasoning=%s", raw)
	}
}
