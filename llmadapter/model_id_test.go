package llmadapter

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestNormalizeModelID(t *testing.T) {
	if got := NormalizeModelID("  Qwen/Qwen2.5-7B  "); got != "qwen/qwen2.5-7b" {
		t.Fatalf("NormalizeModelID=%q", got)
	}
	if got := NormalizeModelID(""); got != "" {
		t.Fatalf("empty NormalizeModelID=%q", got)
	}
}

func TestValidateHuggingFaceModelID(t *testing.T) {
	if err := ValidateHuggingFaceModelID("Qwen/Qwen2.5-7B"); err != nil {
		t.Fatalf("expected valid id: %v", err)
	}
	if err := ValidateHuggingFaceModelID("bert-base-uncased"); err != nil {
		t.Fatalf("expected single-segment id: %v", err)
	}
	if err := ValidateHuggingFaceModelID("https://huggingface.co/org/model"); err == nil {
		t.Fatal("expected URL rejection")
	}
	if err := ValidateHuggingFaceModelID("http://example.com/model.safetensors"); err == nil {
		t.Fatal("expected URL rejection")
	}
	if err := ValidateHuggingFaceModelID("org/model with space"); err == nil {
		t.Fatal("expected whitespace rejection")
	}
	if err := ValidateHuggingFaceModelID(""); err == nil {
		t.Fatal("expected empty rejection")
	}
}

func TestBuildChatCompletionsTaskArgsNormalizesModel(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen2.5-7B",
		"messages":[{"role":"user","content":"hi"}]
	}`)
	taskArgsJSON, meta, err := BuildChatCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "qwen/qwen2.5-7b" {
		t.Fatalf("meta.Model=%q", meta.Model)
	}
	var args models.GPTTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if args.Model != "qwen/qwen2.5-7b" {
		t.Fatalf("task_args.model=%q", args.Model)
	}
	if args.DType != "" {
		t.Fatalf("dtype=%q", args.DType)
	}
}

func TestBuildCompletionsTaskArgsNormalizesModel(t *testing.T) {
	body := []byte(`{
		"model":"Qwen/Qwen3-8B",
		"prompt":"hi"
	}`)
	taskArgsJSON, meta, err := BuildCompletionsTaskArgs(body, 128)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "qwen/qwen3-8b" {
		t.Fatalf("meta.Model=%q", meta.Model)
	}
	var args models.GPTTaskArgs
	if err := json.Unmarshal([]byte(taskArgsJSON), &args); err != nil {
		t.Fatal(err)
	}
	if args.Model != "qwen/qwen3-8b" {
		t.Fatalf("task_args.model=%q", args.Model)
	}
	if args.DType != "" {
		t.Fatalf("dtype=%q", args.DType)
	}
}

func TestParseResponsesRequestNormalizesModel(t *testing.T) {
	req, err := ParseResponsesRequest([]byte(`{
		"model":"Qwen/Qwen2.5-7B",
		"input":"hi"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "qwen/qwen2.5-7b" {
		t.Fatalf("req.Model=%q", req.Model)
	}
}
