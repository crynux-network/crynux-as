package service

import (
	"context"
	"crypto/sha256"
	"crynux_as/config"
	"crynux_as/relay"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testCacheHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

func testCacheEntry(repository, format string, formatKwargs map[string]any) map[string]any {
	return map[string]any{
		"source_type":   "huggingface",
		"repository":    repository,
		"format":        format,
		"format_kwargs": formatKwargs,
	}
}

// testLLMRow builds a Relay loaded-models LLM row with a consistent model_cache_config.
func testLLMRow(seed, model, dtype string, quant any, minVRAM, inMemory, onDisk int) map[string]any {
	return map[string]any{
		"model_id":            model,
		"model_type":          "llm",
		"model_cache_hash":    testCacheHash(seed),
		"execution_dtype":     dtype,
		"quantization_config": quant,
		"model_cache_config": map[string]any{
			"task_kind":           "llm",
			"model":               testCacheEntry(model, "", map[string]any{}),
			"dtype":               dtype,
			"quantization_config": quant,
		},
		"min_vram":             minVRAM,
		"in_memory_node_count": inMemory,
		"on_disk_node_count":   onDisk,
	}
}

// testImageRow builds a Relay loaded-models Image row with a consistent model_cache_config.
// components maps usage name to a canonical entry that uses "repository".
func testImageRow(seed, model, dtype string, quant any, components map[string]any, lora any, minVRAM, inMemory, onDisk int) map[string]any {
	if components == nil {
		components = map[string]any{}
	}
	return map[string]any{
		"model_id":            model,
		"model_type":          "sd",
		"model_cache_hash":    testCacheHash(seed),
		"execution_dtype":     dtype,
		"quantization_config": quant,
		"model_cache_config": map[string]any{
			"task_kind":           "image",
			"base_model":          testCacheEntry(model, "diffusers", map[string]any{}),
			"components":          components,
			"lora":                lora,
			"dtype":               dtype,
			"quantization_config": quant,
		},
		"min_vram":             minVRAM,
		"in_memory_node_count": inMemory,
		"on_disk_node_count":   onDisk,
	}
}

// refreshTestLoadedModels serves rows as the Relay loaded-models response and refreshes the caches.
func refreshTestLoadedModels(t *testing.T, minCatalogOnDisk uint64, defaultVram uint64, rows ...map[string]any) {
	t.Helper()
	cfg := &config.AppConfig{}
	cfg.LLM.MinCatalogOnDiskNodeCount = minCatalogOnDisk
	cfg.LLM.DefaultVramLimit = defaultVram
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"message": "success", "data": rows})
	}))
	t.Cleanup(server.Close)

	InitLoadedModelsCache(relay.NewClient(server.URL))
	if err := RefreshLoadedModels(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLoadedModelCachesSeparateLLMAndImageModels(t *testing.T) {
	awq := map[string]any{"quant_method": "awq", "quant_kwargs": map[string]any{}}
	refreshTestLoadedModels(t, 3, 24,
		testImageRow("z-image", "z/image", "float16", nil, nil, nil, 16, 2, 5),
		testLLMRow("llm-bf16", "a/llm", "bfloat16", nil, 24, 3, 4),
		testLLMRow("llm-awq", "a/llm", "float16", awq, 12, 1, 4),
		testImageRow("a-image-fp16", "a/image", "float16", nil, nil, nil, 8, 1, 3),
		testImageRow("a-image-bf16", "a/image", "bfloat16", nil, nil, nil, 12, 4, 6),
	)

	llmModels := ListLoadedLLMModels()
	if len(llmModels) != 2 {
		t.Fatalf("LLM models = %+v", llmModels)
	}
	got, ok := GetLoadedLLMModel("a/llm")
	if !ok || got.MinVRAM != 24 || got.ExecutionDType != "bfloat16" {
		t.Fatalf("GetLoadedLLMModel max min_vram = %+v %v", got, ok)
	}
	if got.ModelCacheHash != testCacheHash("llm-bf16") || got.TaskKind != "llm" {
		t.Fatalf("LLM identity = %+v", got)
	}
	exact, ok := GetLoadedLLMModelExact("a/llm", awq, "float16")
	if !ok || exact.MinVRAM != 12 {
		t.Fatalf("GetLoadedLLMModelExact = %+v %v", exact, ok)
	}

	imageModels := ListLoadedSDModels()
	if len(imageModels) != 3 {
		t.Fatalf("image models = %+v", imageModels)
	}
	if imageModels[0].ModelID != "a/image" || imageModels[0].ExecutionDType != "bfloat16" {
		t.Fatalf("first image model = %+v", imageModels[0])
	}
	if imageModels[1].ModelID != "a/image" || imageModels[1].ExecutionDType != "float16" || imageModels[1].MinVRAM != 8 {
		t.Fatalf("second image model = %+v", imageModels[1])
	}
	if imageModels[2].ModelID != "z/image" {
		t.Fatalf("third image model = %+v", imageModels[2])
	}
	if _, ok := GetLoadedLLMModel("a/image"); ok {
		t.Fatal("image model leaked into LLM cache")
	}
	if _, ok := GetLoadedSDModel("a/llm"); ok {
		t.Fatal("LLM model leaked into image cache")
	}
	got, ok = GetLoadedSDModel("A/Image")
	if !ok || got.MinVRAM != 12 {
		t.Fatalf("GetLoadedSDModel max min_vram = %+v %v", got, ok)
	}
}

func TestLoadedModelCatalogFiltersByOnDiskNodeCount(t *testing.T) {
	refreshTestLoadedModels(t, 3, 24,
		testLLMRow("keep-llm", "keep/llm", "bfloat16", nil, 24, 1, 3),
		testLLMRow("drop-llm", "drop/llm", "bfloat16", nil, 16, 9, 2),
		testImageRow("keep-image", "keep/image", "float16", nil, nil, nil, 12, 1, 5),
		testImageRow("drop-image", "drop/image", "float16", nil, nil, nil, 8, 4, 1),
	)

	llmModels := ListLoadedLLMModels()
	if len(llmModels) != 1 || llmModels[0].ModelID != "keep/llm" {
		t.Fatalf("catalog LLM models = %+v", llmModels)
	}
	if _, ok := GetCatalogLLMModel("keep/llm"); !ok {
		t.Fatal("expected keep/llm in catalog snapshot")
	}
	if _, ok := GetCatalogLLMModel("drop/llm"); ok {
		t.Fatal("drop/llm must not appear in catalog snapshot")
	}
	got, ok := GetLoadedLLMModel("drop/llm")
	if !ok || got.OnDiskNodeCount != 2 || got.MinVRAM != 16 {
		t.Fatalf("full LLM cache missing drop/llm: %+v %v", got, ok)
	}

	imageModels := ListLoadedSDModels()
	if len(imageModels) != 1 || imageModels[0].ModelID != "keep/image" {
		t.Fatalf("catalog image models = %+v", imageModels)
	}
	got, ok = GetLoadedSDModel("drop/image")
	if !ok || got.OnDiskNodeCount != 1 || got.MinVRAM != 8 {
		t.Fatalf("full image cache missing drop/image: %+v %v", got, ok)
	}
}

func TestImageRowsDifferingOnlyByComponentRepoAreRetained(t *testing.T) {
	componentsA := map[string]any{"transformer": testCacheEntry("org/quant-a", "gguf", map[string]any{"filename": "a.gguf"})}
	componentsB := map[string]any{"transformer": testCacheEntry("org/Quant-B", "gguf", map[string]any{"filename": "a.gguf"})}
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("image-a", "org/image", "bfloat16", nil, componentsA, nil, 20, 1, 2),
		testImageRow("image-b", "org/image", "bfloat16", nil, componentsB, nil, 16, 3, 4),
	)

	imageModels := ListLoadedSDModels()
	if len(imageModels) != 2 {
		t.Fatalf("image models = %+v", imageModels)
	}
	byHash := map[string]LoadedLLMModel{}
	for _, model := range imageModels {
		byHash[model.ModelCacheHash] = model
	}
	a, b := byHash[testCacheHash("image-a")], byHash[testCacheHash("image-b")]
	if a.MinVRAM != 20 || a.NodeCount != 1 || a.OnDiskNodeCount != 2 {
		t.Fatalf("row a = %+v", a)
	}
	if b.MinVRAM != 16 || b.NodeCount != 3 || b.OnDiskNodeCount != 4 {
		t.Fatalf("row b = %+v", b)
	}
	transformerA := a.Components["transformer"].(map[string]any)
	transformerB := b.Components["transformer"].(map[string]any)
	if transformerA["repo"] != "org/quant-a" || transformerB["repo"] != "org/quant-b" {
		t.Fatalf("components a = %#v b = %#v", a.Components, b.Components)
	}
	if _, ok := transformerA["repository"]; ok {
		t.Fatalf("components must use the TaskArgs repo field: %#v", transformerA)
	}
}

func TestInconsistentLoadedModelRowsAreSkipped(t *testing.T) {
	missingHash := testLLMRow("missing-hash", "org/llm", "auto", nil, 8, 1, 1)
	delete(missingHash, "model_cache_hash")
	missingConfig := testLLMRow("missing-config", "org/llm", "auto", nil, 8, 1, 1)
	delete(missingConfig, "model_cache_config")
	shortHash := testLLMRow("short-hash", "org/llm", "auto", nil, 8, 1, 1)
	shortHash["model_cache_hash"] = "abcd"
	wrongModel := testLLMRow("wrong-model", "org/llm", "auto", nil, 8, 1, 1)
	wrongModel["model_id"] = "org/other"
	wrongDType := testLLMRow("wrong-dtype", "org/llm", "auto", nil, 8, 1, 1)
	wrongDType["execution_dtype"] = "float16"
	wrongQuant := testLLMRow("wrong-quant", "org/llm", "auto", nil, 8, 1, 1)
	wrongQuant["quantization_config"] = map[string]any{"quant_method": "awq", "quant_kwargs": map[string]any{}}
	wrongKind := testImageRow("wrong-kind", "org/image", "auto", nil, nil, nil, 8, 1, 1)
	wrongKind["model_type"] = "llm"
	incompleteComponent := testImageRow("incomplete-component", "org/image", "auto", nil,
		map[string]any{"transformer": testCacheEntry("", "gguf", nil)}, nil, 8, 1, 1)
	good := testLLMRow("good", "org/good", "auto", nil, 8, 1, 1)

	refreshTestLoadedModels(t, 0, 24,
		missingHash, missingConfig, shortHash, wrongModel, wrongDType, wrongQuant, wrongKind, incompleteComponent, good,
	)

	llmModels := ListLoadedLLMModels()
	if len(llmModels) != 1 || llmModels[0].ModelID != "org/good" {
		t.Fatalf("LLM models = %+v", llmModels)
	}
	if models := ListLoadedSDModels(); len(models) != 0 {
		t.Fatalf("image models = %+v", models)
	}
}
