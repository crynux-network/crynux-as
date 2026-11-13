package service

import (
	"crynux_as/config"
	"strings"
	"testing"
)

func TestModelNotReadyRetryMinutes(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.LoadedModelsRefreshInterval = 1800
	cfg.LLM.MinCatalogOnDiskNodeCount = 3
	cfg.LLM.DefaultVramLimit = 24
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	if got := ModelNotReadyRetryMinutes(); got != 30 {
		t.Fatalf("1800s -> %d minutes, want 30", got)
	}
	cfg.LLM.LoadedModelsRefreshInterval = 61
	if got := ModelNotReadyRetryMinutes(); got != 2 {
		t.Fatalf("61s -> %d minutes, want 2", got)
	}
	cfg.LLM.LoadedModelsRefreshInterval = 1800
	msg := ModelNotReadyMessage()
	if !strings.Contains(msg, "Please try again in 30 minutes.") {
		t.Fatalf("message=%q", msg)
	}
}

func TestCrynuxLLMModelReadyIgnoresQuantization(t *testing.T) {
	refreshTestLoadedModels(t, 3, 24,
		testLLMRow("a", "Org/Model", "float16", map[string]any{"quant_method": "awq"}, 24, 0, 0),
		testLLMRow("b", "Org/Model", "bfloat16", nil, 24, 0, 3),
	)

	if !CrynuxLLMModelReady("ORG/MODEL") {
		t.Fatal("expected ready when any row for model_id has on_disk>0")
	}
	if CrynuxLLMModelReady("other/model") {
		t.Fatal("expected not ready for missing model")
	}
}

func TestCrynuxImageTaskArgsReadyRequiresSameComponentsSet(t *testing.T) {
	baseOnly := testImageRow("base", "acme/base", "float16", nil, map[string]any{}, nil, 24, 0, 5)
	withComponent := testImageRow(
		"comp",
		"acme/base",
		"float16",
		nil,
		map[string]any{
			"transformer": map[string]any{
				"format":     "diffusers",
				"repository": "acme/transformer-v2",
			},
		},
		nil,
		24,
		0,
		0,
	)
	withComponentReady := testImageRow(
		"comp-ready",
		"acme/base",
		"bfloat16",
		map[string]any{"quant_method": "bitsandbytes"},
		map[string]any{
			"transformer": map[string]any{
				"format":     "diffusers",
				"repository": "acme/transformer-v2",
			},
		},
		map[string]any{"model": "acme/lora", "weight_file_name": "w.safetensors", "weight": "100"},
		24,
		0,
		2,
	)
	refreshTestLoadedModels(t, 3, 24, baseOnly, withComponent, withComponentReady)

	baseArgs := `{"base_model":{"name":"Acme/Base"},"components":{}}`
	if !CrynuxImageTaskArgsReady(baseArgs) {
		t.Fatal("base-only request should match base-only on_disk row")
	}

	newCompArgs := `{
		"base_model":{"name":"acme/base"},
		"components":{"transformer":{"format":"diffusers","repo":"acme/transformer-v2"}},
		"lora":{"model":"acme/other-lora","weight_file_name":"x.safetensors","weight":"50"}
	}`
	if !CrynuxImageTaskArgsReady(newCompArgs) {
		t.Fatal("same components set with on_disk>0 must be ready; lora/quant differences ignored")
	}

	missingCompArgs := `{
		"base_model":{"name":"acme/base"},
		"components":{"transformer":{"format":"diffusers","repo":"acme/transformer-missing"}}
	}`
	if CrynuxImageTaskArgsReady(missingCompArgs) {
		t.Fatal("different components set must not borrow base-only on_disk coverage")
	}
}
