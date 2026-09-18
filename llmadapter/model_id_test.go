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
	if args.DType != models.DTypeBFloat16 {
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
	if args.DType != models.DTypeAuto {
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

func TestResolveDTypeQwen25(t *testing.T) {
	if got := resolveDType("qwen/qwen2.5-7b"); got != models.DTypeBFloat16 {
		t.Fatalf("qwen2.5 dtype=%q", got)
	}
	if got := resolveDType("qwen/qwen3-8b"); got != models.DTypeAuto {
		t.Fatalf("qwen3 dtype=%q", got)
	}
}
