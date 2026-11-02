package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestCorrectFinishReasonForLengthCap(t *testing.T) {
	if got := correctFinishReasonForLengthCap("length", 54, 768); got != "stop" {
		t.Fatalf("early stop length -> stop, got %q", got)
	}
	if got := correctFinishReasonForLengthCap("length", 768, 768); got != "length" {
		t.Fatalf("at cap keeps length, got %q", got)
	}
	if got := correctFinishReasonForLengthCap("length", 800, 768); got != "length" {
		t.Fatalf("over cap keeps length, got %q", got)
	}
	if got := correctFinishReasonForLengthCap("stop", 54, 768); got != "stop" {
		t.Fatalf("stop unchanged, got %q", got)
	}
	if got := correctFinishReasonForLengthCap("tool_calls", 54, 768); got != "tool_calls" {
		t.Fatalf("tool_calls unchanged, got %q", got)
	}
	if got := correctFinishReasonForLengthCap("length", 54, 0); got != "length" {
		t.Fatalf("max 0 does not rewrite, got %q", got)
	}
}

func TestFormatChatCompletionsRewritesFalseLength(t *testing.T) {
	raw := &models.GPTTaskResponse{
		Model: "qwen/qwen3-8b",
		Choices: []models.ResponseChoice{{
			Index:        0,
			Message:      models.Message{Role: models.LLMRoleAssistant, Content: "hello"},
			FinishReason: models.FinishReasonLength,
		}},
		Usage: models.Usage{PromptTokens: 10, CompletionTokens: 54, TotalTokens: 64},
	}
	taskArgs := &models.GPTTaskArgs{
		GenerationConfig: &models.GPTGenerationConfig{MaxNewTokens: 768},
	}

	formatted, err := FormatChatCompletionsResponseWithTaskArgs(raw, "chatcmpl-1", 1, taskArgs)
	if err != nil {
		t.Fatal(err)
	}
	var resp ChatCompletionsResponse
	if err := json.Unmarshal(formatted, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason=%q", resp.Choices[0].FinishReason)
	}
}

func TestFormatChatCompletionsKeepsLengthAtCap(t *testing.T) {
	raw := &models.GPTTaskResponse{
		Model: "qwen/qwen3-8b",
		Choices: []models.ResponseChoice{{
			Index:        0,
			Message:      models.Message{Role: models.LLMRoleAssistant, Content: "x"},
			FinishReason: models.FinishReasonLength,
		}},
		Usage: models.Usage{PromptTokens: 10, CompletionTokens: 768, TotalTokens: 778},
	}
	taskArgs := &models.GPTTaskArgs{
		GenerationConfig: &models.GPTGenerationConfig{MaxNewTokens: 768},
	}

	formatted, err := FormatChatCompletionsResponseWithTaskArgs(raw, "chatcmpl-2", 1, taskArgs)
	if err != nil {
		t.Fatal(err)
	}
	var resp ChatCompletionsResponse
	if err := json.Unmarshal(formatted, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].FinishReason != "length" {
		t.Fatalf("finish_reason=%q", resp.Choices[0].FinishReason)
	}
}

func TestFormatChatCompletionsKeepsToolCallsOverLengthRewrite(t *testing.T) {
	raw := &models.GPTTaskResponse{
		Model: "qwen/qwen3-8b",
		Choices: []models.ResponseChoice{{
			Index: 0,
			Message: models.Message{
				Role:    models.LLMRoleAssistant,
				Content: `<tool_call>{"name":"weather","arguments":{"city":"Paris"}}</tool_call>`,
			},
			FinishReason: models.FinishReasonLength,
		}},
		Usage: models.Usage{PromptTokens: 10, CompletionTokens: 54, TotalTokens: 64},
	}
	taskArgs := &models.GPTTaskArgs{
		Tools: []map[string]interface{}{{
			"type":     "function",
			"function": map[string]interface{}{"name": "weather"},
		}},
		ToolChoice:       "required",
		GenerationConfig: &models.GPTGenerationConfig{MaxNewTokens: 768},
	}

	formatted, err := FormatChatCompletionsResponseWithTaskArgs(raw, "chatcmpl-3", 1, taskArgs)
	if err != nil {
		t.Fatal(err)
	}
	var resp ChatCompletionsResponse
	if err := json.Unmarshal(formatted, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("finish_reason=%q", resp.Choices[0].FinishReason)
	}
}

func TestFormatChatCompletionsSkipsRewriteWithoutTaskArgs(t *testing.T) {
	raw := &models.GPTTaskResponse{
		Model: "qwen/qwen3-8b",
		Choices: []models.ResponseChoice{{
			Index:        0,
			Message:      models.Message{Role: models.LLMRoleAssistant, Content: "hello"},
			FinishReason: models.FinishReasonLength,
		}},
		Usage: models.Usage{PromptTokens: 10, CompletionTokens: 54, TotalTokens: 64},
	}

	formatted, err := FormatChatCompletionsResponseWithTaskArgs(raw, "chatcmpl-4", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	var resp ChatCompletionsResponse
	if err := json.Unmarshal(formatted, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].FinishReason != "length" {
		t.Fatalf("finish_reason=%q", resp.Choices[0].FinishReason)
	}
}

func TestFormatCompletionsRewritesFalseLength(t *testing.T) {
	raw := &models.GPTTaskResponse{
		Model: "qwen/qwen3-8b",
		Choices: []models.ResponseChoice{{
			Index:        0,
			Message:      models.Message{Role: models.LLMRoleAssistant, Content: "hello"},
			FinishReason: models.FinishReasonLength,
		}},
		Usage: models.Usage{PromptTokens: 10, CompletionTokens: 54, TotalTokens: 64},
	}
	taskArgs := &models.GPTTaskArgs{
		GenerationConfig: &models.GPTGenerationConfig{MaxNewTokens: 768},
	}

	formatted, err := FormatCompletionsResponse(raw, "cmpl-1", 1, taskArgs)
	if err != nil {
		t.Fatal(err)
	}
	var resp CompletionsResponse
	if err := json.Unmarshal(formatted, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("finish_reason=%q", resp.Choices[0].FinishReason)
	}
}
