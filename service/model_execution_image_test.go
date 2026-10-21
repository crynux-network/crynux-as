package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/relay"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplyImageExecutionPreservesClientQuantizationAndComponents(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.DefaultVramLimit = 24
	cfg.LLM.MinCatalogOnDiskNodeCount = 1
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	userQuant := map[string]any{
		"quant_method":           "bitsandbytes",
		"components_to_quantize": []any{"text_encoder"},
		"quant_kwargs":           map[string]any{"load_in_8bit": true},
	}
	cacheQuant := map[string]any{
		"quant_method":           "bitsandbytes",
		"components_to_quantize": []any{"transformer"},
		"quant_kwargs":           map[string]any{"load_in_4bit": true},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "success",
			"data": []map[string]interface{}{
				{
					"model_id":             "qwen/qwen-image-2.1",
					"model_type":           "sd",
					"execution_dtype":      "bfloat16",
					"quantization_config":  cacheQuant,
					"min_vram":             20,
					"in_memory_node_count": 1,
					"on_disk_node_count":   2,
				},
				{
					"model_id":             "qwen/qwen-image-2.1",
					"model_type":           "sd",
					"execution_dtype":      "bfloat16",
					"quantization_config":  userQuant,
					"min_vram":             16,
					"in_memory_node_count": 1,
					"on_disk_node_count":   2,
				},
			},
		})
	}))
	defer server.Close()

	InitLoadedModelsCache(relay.NewClient(server.URL))
	if err := RefreshLoadedModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	taskArgs := `{
		"base_model": {"name": "qwen/qwen-image-2.1"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"quantization_config": {
			"quant_method": "bitsandbytes",
			"components_to_quantize": ["text_encoder"],
			"quant_kwargs": {"load_in_8bit": true}
		},
		"components": {
			"transformer": {
				"format": "gguf",
				"repo": "abenzerps/Qwen-Image-2.1-Uncensored-Quantized",
				"format_kwargs": {"filename": "qwen-image-2.1-UC-Q4_K_M.gguf"}
			}
		}
	}`
	out, minVRAM, err := ApplyImageExecutionToTaskArgsJSON(taskArgs, "Qwen/Qwen-Image-2.1")
	if err != nil {
		t.Fatal(err)
	}
	if minVRAM != 16 {
		t.Fatalf("minVRAM = %d", minVRAM)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["dtype"] != "bfloat16" {
		t.Fatalf("dtype = %#v", payload["dtype"])
	}
	quant, ok := payload["quantization_config"].(map[string]any)
	if !ok {
		t.Fatalf("quantization_config = %#v", payload["quantization_config"])
	}
	componentsToQuantize, _ := quant["components_to_quantize"].([]any)
	if len(componentsToQuantize) != 1 || componentsToQuantize[0] != "text_encoder" {
		t.Fatalf("client quant overwritten: %#v", quant)
	}
	components, ok := payload["components"].(map[string]any)
	if !ok {
		t.Fatalf("components missing: %#v", payload["components"])
	}
	transformer, ok := components["transformer"].(map[string]any)
	if !ok || transformer["repo"] != "abenzerps/Qwen-Image-2.1-Uncensored-Quantized" {
		t.Fatalf("components = %#v", components)
	}
}

func TestApplyImageExecutionInjectsCacheQuantWhenOmitted(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.DefaultVramLimit = 24
	cfg.LLM.MinCatalogOnDiskNodeCount = 1
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	cacheQuant := map[string]any{
		"quant_method":           "bitsandbytes",
		"components_to_quantize": []any{"transformer"},
		"quant_kwargs":           map[string]any{"load_in_4bit": true},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "success",
			"data": []map[string]interface{}{
				{
					"model_id":             "org/image",
					"model_type":           "sd",
					"execution_dtype":      "float16",
					"quantization_config":  cacheQuant,
					"min_vram":             12,
					"in_memory_node_count": 1,
					"on_disk_node_count":   2,
				},
			},
		})
	}))
	defer server.Close()

	InitLoadedModelsCache(relay.NewClient(server.URL))
	if err := RefreshLoadedModels(context.Background()); err != nil {
		t.Fatal(err)
	}

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
	}`
	out, minVRAM, err := ApplyImageExecutionToTaskArgsJSON(taskArgs, "org/image")
	if err != nil {
		t.Fatal(err)
	}
	if minVRAM != 12 {
		t.Fatalf("minVRAM = %d", minVRAM)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["dtype"] != "float16" {
		t.Fatalf("dtype = %#v", payload["dtype"])
	}
	quant, ok := payload["quantization_config"].(map[string]any)
	if !ok {
		t.Fatalf("quantization_config = %#v", payload["quantization_config"])
	}
	componentsToQuantize, _ := quant["components_to_quantize"].([]any)
	if len(componentsToQuantize) != 1 || componentsToQuantize[0] != "transformer" {
		t.Fatalf("cache quant missing: %#v", quant)
	}
}
