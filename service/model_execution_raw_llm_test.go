package service

import (
	"encoding/json"
	"errors"
	"testing"
)

func resolveRawLLM(t *testing.T, taskArgs string) (map[string]any, TaskExecutionResolution) {
	t.Helper()
	resolved, err := ResolveRawLLMModelExecution(taskArgs, "org/llm")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(resolved.TaskArgsJSON), &payload); err != nil {
		t.Fatal(err)
	}
	return payload, resolved
}

func TestResolveRawLLMRejectsNonConcreteDType(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24, testLLMRow("org/llm", "bfloat16", nil, 20, 1, 2))
	for _, taskArgs := range []string{
		`{"version":"3.8.0","model":"org/llm","messages":[],"dtype":"auto"}`,
		`{"version":"3.8.0","model":"org/llm","messages":[],"dtype":null}`,
		`{"version":"3.8.0","model":"org/llm","messages":[],"dtype":"int8"}`,
	} {
		if _, err := ResolveRawLLMModelExecution(taskArgs, "org/llm"); !errors.Is(err, ErrInvalidExecutionConfig) {
			t.Fatalf("%s: error = %v", taskArgs, err)
		}
	}
}

func TestResolveRawLLMOmittedDTypeUsesLoadedRow(t *testing.T) {
	awq := map[string]any{"quant_method": "awq", "quant_kwargs": map[string]any{}}
	refreshTestLoadedModels(t, 1, 24,
		testLLMRow("org/llm", "bfloat16", nil, 20, 1, 2),
		testLLMRow("org/llm", "float16", awq, 40, 1, 2),
	)
	payload, resolved := resolveRawLLM(t, `{"version":"3.8.0","model":"Org/LLM","messages":[]}`)
	if payload["dtype"] != "float16" {
		t.Fatalf("dtype = %#v", payload["dtype"])
	}
	quant, ok := payload["quantization_config"].(map[string]any)
	if !ok || quant["quant_method"] != "awq" {
		t.Fatalf("quantization_config = %#v", payload["quantization_config"])
	}
	if payload["version"] != "3.8.0" || payload["model"] != "org/llm" {
		t.Fatalf("version/model = %#v %#v", payload["version"], payload["model"])
	}
	if resolved.MinVRAM != 40 || !resolved.FromLoadedModels {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveRawLLMOmittedDTypeMissUsesBFloat16(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24)
	payload, resolved := resolveRawLLM(t, `{"version":"3.7.1","model":"org/llm","messages":[]}`)
	if payload["dtype"] != "bfloat16" {
		t.Fatalf("dtype = %#v", payload["dtype"])
	}
	if value, ok := payload["quantization_config"]; !ok || value != nil {
		t.Fatalf("quantization_config = %#v, present = %t", value, ok)
	}
	if resolved.MinVRAM != 24 || resolved.FromLoadedModels {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveRawLLMKeepsConcreteDType(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testLLMRow("org/llm", "bfloat16", nil, 20, 1, 2),
		testLLMRow("org/llm", "float32", nil, 32, 1, 2),
	)
	payload, resolved := resolveRawLLM(t, `{"version":"3.7.1","model":"org/llm","messages":[],"dtype":"Float32"}`)
	if payload["dtype"] != "float32" {
		t.Fatalf("dtype = %#v", payload["dtype"])
	}
	if resolved.MinVRAM != 32 {
		t.Fatalf("min_vram = %d", resolved.MinVRAM)
	}
}
