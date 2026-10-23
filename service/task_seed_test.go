package service

import (
	"crynux_as/models"
	"encoding/json"
	"testing"
)

func TestEnsureTaskSeedLLMOmitsThenFills(t *testing.T) {
	out, err := EnsureTaskSeed(`{"model":"org/model","messages":[]}`, models.TaskTypeLLM)
	if err != nil {
		t.Fatal(err)
	}
	seed := requireLLMSeed(t, out)
	if seed < 0 || seed >= taskSeedExclusiveMax {
		t.Fatalf("seed=%d", seed)
	}
}

func TestEnsureTaskSeedLLMKeepsExplicitZero(t *testing.T) {
	in := `{"model":"org/model","seed":0}`
	out, err := EnsureTaskSeed(in, models.TaskTypeLLM)
	if err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("task args = %s", out)
	}
}

func TestEnsureTaskSeedImageFillsMissingAndKeepsExplicit(t *testing.T) {
	out, err := EnsureTaskSeed(`{"prompt":"p","task_config":{"steps":20}}`, models.TaskTypeImage)
	if err != nil {
		t.Fatal(err)
	}
	seed := requireImageSeed(t, out)
	if seed < 0 || seed >= taskSeedExclusiveMax {
		t.Fatalf("seed=%d", seed)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	cfg := payload["task_config"].(map[string]any)
	if cfg["steps"] != float64(20) {
		t.Fatalf("steps=%v", cfg["steps"])
	}

	in := `{"prompt":"p","task_config":{"seed":0,"steps":1}}`
	kept, err := EnsureTaskSeed(in, models.TaskTypeImage)
	if err != nil {
		t.Fatal(err)
	}
	if kept != in {
		t.Fatalf("task args = %s", kept)
	}
}

func TestEnsureTaskSeedImageWithoutTaskConfig(t *testing.T) {
	out, err := EnsureTaskSeed(`{"prompt":"p"}`, models.TaskTypeImage)
	if err != nil {
		t.Fatal(err)
	}
	seed := requireImageSeed(t, out)
	if seed < 0 || seed >= taskSeedExclusiveMax {
		t.Fatalf("seed=%d", seed)
	}
}

func requireLLMSeed(t *testing.T, taskArgs string) int64 {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgs), &payload); err != nil {
		t.Fatal(err)
	}
	seed, ok := payload["seed"].(float64)
	if !ok || seed != float64(int64(seed)) {
		t.Fatalf("seed=%v", payload["seed"])
	}
	return int64(seed)
}

func requireImageSeed(t *testing.T, taskArgs string) int64 {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(taskArgs), &payload); err != nil {
		t.Fatal(err)
	}
	cfg, ok := payload["task_config"].(map[string]any)
	if !ok {
		t.Fatalf("task_config=%v", payload["task_config"])
	}
	seed, ok := cfg["seed"].(float64)
	if !ok || seed != float64(int64(seed)) {
		t.Fatalf("seed=%v", cfg["seed"])
	}
	return int64(seed)
}
