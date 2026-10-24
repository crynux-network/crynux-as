package service

import (
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestNormalizeModelName(t *testing.T) {
	if got := llmadapter.NormalizeModelName("Qwen/Qwen-Image-2.1"); got != "qwen/qwen-image-2.1" {
		t.Fatalf("got %q", got)
	}
	url := "https://Example.com/Org/Repo"
	if got := llmadapter.NormalizeModelName(url); got != url {
		t.Fatalf("url got %q", got)
	}
}

func TestNormalizeTaskArgsModelNamesLLM(t *testing.T) {
	out, err := NormalizeTaskArgsModelNames(
		`{"model":"Qwen/Qwen3.5-9B","dtype":"bfloat16","seed":1}`,
		models.TaskTypeLLM,
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "qwen/qwen3.5-9b" {
		t.Fatalf("model = %#v", payload["model"])
	}
}

func TestNormalizeTaskArgsModelNamesImage(t *testing.T) {
	out, err := NormalizeTaskArgsModelNames(`{
		"base_model": {"name": "Qwen/Qwen-Image-2.1"},
		"lora": {"model": "Org/LoRA"},
		"components": {
			"transformer": {"format": "diffusers", "repo": "Org/Transformer"}
		},
		"task_config": {"steps": 20}
	}`, models.TaskTypeImage)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	base := payload["base_model"].(map[string]any)
	if base["name"] != "qwen/qwen-image-2.1" {
		t.Fatalf("base_model.name = %#v", base["name"])
	}
	lora := payload["lora"].(map[string]any)
	if lora["model"] != "org/lora" {
		t.Fatalf("lora.model = %#v", lora["model"])
	}
	components := payload["components"].(map[string]any)
	transformer := components["transformer"].(map[string]any)
	if transformer["repo"] != "org/transformer" {
		t.Fatalf("components.transformer.repo = %#v", transformer["repo"])
	}
}

func TestEnsureTaskSeedNormalizesWhenSeedPresent(t *testing.T) {
	out, err := EnsureTaskSeed(
		`{"model":"Org/LLM","seed":0}`,
		models.TaskTypeLLM,
	)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "org/llm" {
		t.Fatalf("model = %#v", payload["model"])
	}
}
