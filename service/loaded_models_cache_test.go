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

// testComponent builds a component in the TaskArgs shape returned by Relay loaded-models.
func testComponent(repository, format string, formatKwargs map[string]any) map[string]any {
	if formatKwargs == nil {
		formatKwargs = map[string]any{}
	}
	return map[string]any{
		"repo":          repository,
		"format":        format,
		"format_kwargs": formatKwargs,
	}
}

// testLLMRow builds a Relay loaded-models LLM row.
func testLLMRow(model, dtype string, quant any, minVRAM, inMemory, onDisk int) map[string]any {
	return map[string]any{
		"model_id":             model,
		"model_type":           "llm",
		"dtype":                dtype,
		"quantization_config":  quant,
		"min_vram":             minVRAM,
		"in_memory_node_count": inMemory,
		"on_disk_node_count":   onDisk,
	}
}

// testImageRow builds a Relay loaded-models Image row with components and lora in the TaskArgs shape.
func testImageRow(model, dtype string, quant any, components map[string]any, lora any, minVRAM, inMemory, onDisk int) map[string]any {
	if components == nil {
		components = map[string]any{}
	}
	return map[string]any{
		"model_id":             model,
		"model_type":           "sd",
		"dtype":                dtype,
		"quantization_config":  quant,
		"components":           components,
		"lora":                 lora,
		"min_vram":             minVRAM,
		"in_memory_node_count": inMemory,
		"on_disk_node_count":   onDisk,
	}
}

// testVideoRow builds a Relay loaded-models Video row with components and loras in the TaskArgs shape.
func testVideoRow(model, dtype string, quant any, components map[string]any, loras []any, minVRAM, inMemory, onDisk int) map[string]any {
	if components == nil {
		components = map[string]any{}
	}
	if loras == nil {
		loras = []any{}
	}
	return map[string]any{
		"model_id":             model,
		"model_type":           "video",
		"dtype":                dtype,
		"quantization_config":  quant,
		"components":           components,
		"loras":                loras,
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
		testImageRow("z/image", "float16", nil, nil, nil, 16, 2, 5),
		testLLMRow("a/llm", "bfloat16", nil, 24, 3, 4),
		testLLMRow("a/llm", "float16", awq, 12, 1, 4),
		testImageRow("a/image", "float16", nil, nil, nil, 8, 1, 3),
		testImageRow("a/image", "bfloat16", nil, nil, nil, 12, 4, 6),
	)

	llmModels := ListLoadedLLMModels()
	if len(llmModels) != 2 {
		t.Fatalf("LLM models = %+v", llmModels)
	}
	got, ok := GetLoadedLLMModel("a/llm")
	if !ok || got.MinVRAM != 24 || got.DType != "bfloat16" {
		t.Fatalf("GetLoadedLLMModel max min_vram = %+v %v", got, ok)
	}
	if len(got.ModelCacheHash) != 64 || got.TaskKind != "llm" {
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
	if imageModels[0].ModelID != "a/image" || imageModels[0].DType != "bfloat16" {
		t.Fatalf("first image model = %+v", imageModels[0])
	}
	if imageModels[1].ModelID != "a/image" || imageModels[1].DType != "float16" || imageModels[1].MinVRAM != 8 {
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
		testLLMRow("keep/llm", "bfloat16", nil, 24, 1, 3),
		testLLMRow("drop/llm", "bfloat16", nil, 16, 9, 2),
		testImageRow("keep/image", "float16", nil, nil, nil, 12, 1, 5),
		testImageRow("drop/image", "float16", nil, nil, nil, 8, 4, 1),
		testVideoRow("keep/video", "bfloat16", nil, nil, nil, 48, 1, 4),
		testVideoRow("drop/video", "bfloat16", nil, nil, nil, 40, 1, 1),
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

	videoModels := ListLoadedVideoModels()
	if len(videoModels) != 1 || videoModels[0].ModelID != "keep/video" {
		t.Fatalf("catalog video models = %+v", videoModels)
	}
	if rows := ListCatalogVideoModelsByID("drop/video"); len(rows) != 0 {
		t.Fatalf("drop/video must not appear in catalog snapshot: %+v", rows)
	}
	got, ok = GetLoadedVideoModel("drop/video")
	if !ok || got.OnDiskNodeCount != 1 || got.MinVRAM != 40 {
		t.Fatalf("full video cache missing drop/video: %+v %v", got, ok)
	}
}

func TestImageRowsDifferingOnlyByComponentRepoAreRetained(t *testing.T) {
	componentsA := map[string]any{"transformer": testComponent("org/quant-a", "gguf", map[string]any{"filename": "a.gguf"})}
	componentsB := map[string]any{"transformer": testComponent("org/Quant-B", "gguf", map[string]any{"filename": "a.gguf"})}
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "bfloat16", nil, componentsA, nil, 20, 1, 2),
		testImageRow("org/image", "bfloat16", nil, componentsB, nil, 16, 3, 4),
	)

	imageModels := ListLoadedSDModels()
	if len(imageModels) != 2 {
		t.Fatalf("image models = %+v", imageModels)
	}
	byRepo := map[string]LoadedLLMModel{}
	for _, model := range imageModels {
		transformer := model.Components["transformer"].(map[string]any)
		byRepo[transformer["repo"].(string)] = model
	}
	a, b := byRepo["org/quant-a"], byRepo["org/quant-b"]
	if a.MinVRAM != 20 || a.NodeCount != 1 || a.OnDiskNodeCount != 2 {
		t.Fatalf("row a = %+v", a)
	}
	if b.MinVRAM != 16 || b.NodeCount != 3 || b.OnDiskNodeCount != 4 {
		t.Fatalf("row b = %+v", b)
	}
	if a.ModelCacheHash == b.ModelCacheHash {
		t.Fatalf("rows with different components share a hash: %s", a.ModelCacheHash)
	}
}

func TestEmptyFormatKwargsMatchesOmittedFormatKwargs(t *testing.T) {
	refreshTestLoadedModels(t, 0, 24,
		testImageRow("org/image", "bfloat16", nil,
			map[string]any{"vae": testComponent("Org/VAE", "diffusers", nil)}, nil, 20, 1, 2),
	)
	row, ok := GetLoadedSDModel("org/image")
	if !ok {
		t.Fatal("expected image row")
	}
	_, key, err := normalizeTaskArgsComponents(map[string]any{
		"vae": map[string]any{"format": "diffusers", "repo": "org/vae"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.ComponentsKey != key {
		t.Fatalf("components key = %s, want %s", row.ComponentsKey, key)
	}
}

func TestVideoLoadedModelsNormalizeLoras(t *testing.T) {
	loras := []any{map[string]any{"model": "Org/LoRA", "weight_file_name": "", "weight": 100, "target": "auto"}}
	refreshTestLoadedModels(t, 0, 24,
		testVideoRow("org/video", "bfloat16", nil, nil, nil, 48, 1, 2),
		testVideoRow("org/video", "bfloat16", nil, nil, loras, 64, 1, 2),
		testImageRow("org/video", "bfloat16", nil, nil, nil, 24, 1, 2),
	)

	got, ok := GetLoadedVideoModel("Org/Video")
	if !ok || got.MinVRAM != 64 || got.TaskKind != "video" {
		t.Fatalf("GetLoadedVideoModel = %+v %v", got, ok)
	}
	if len(got.Loras) != 1 || got.Loras[0].(map[string]any)["model"] != "org/lora" {
		t.Fatalf("loras = %#v", got.Loras)
	}
	_, wantKey, err := normalizeTaskArgsVideoLoras([]any{map[string]any{"model": "org/lora"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.LorasKey != wantKey {
		t.Fatalf("loras key = %s, want %s", got.LorasKey, wantKey)
	}
	plain := ListCatalogVideoModelsByID("org/video")
	if len(plain) != 2 {
		t.Fatalf("video catalog rows = %+v", plain)
	}
	image, ok := GetLoadedSDModel("org/video")
	if !ok || image.ModelCacheHash == plain[0].ModelCacheHash || image.ModelCacheHash == plain[1].ModelCacheHash {
		t.Fatalf("image and video rows must have distinct hashes: %+v %+v", image, plain)
	}
	if !videoModelHasOnDiskNodes("org/video", "{}") {
		t.Fatal("expected on-disk video nodes")
	}
}

func TestModelCacheHashDependsOnIdentity(t *testing.T) {
	base := modelCacheIdentity{TaskKind: "llm", ModelID: "org/llm", DType: "bfloat16", QuantizationKey: "null"}
	first, err := base.hash()
	if err != nil {
		t.Fatal(err)
	}
	second, err := base.hash()
	if err != nil || first != second {
		t.Fatalf("hash is not deterministic: %s %s %v", first, second, err)
	}
	changed := base
	changed.DType = "float16"
	third, err := changed.hash()
	if err != nil || third == first {
		t.Fatalf("dtype must change the hash: %s %s %v", first, third, err)
	}
}

func TestRefreshLoadedModelsSkipsProviderNameCollisions(t *testing.T) {
	if err := InitProviderCatalog(&config.AppConfig{
		Provider: config.ProviderConfig{
			Services: map[string]config.ProviderServiceConfig{
				"openai": {
					DisplayName: "OpenAI",
					Models: []config.ProviderModelConfig{
						{Name: "reserved/llm", APIKind: config.ProviderAPIKindChat, UsageWeights: config.ProviderUsageWeightsConfig{InputTokens: "1", OutputTokens: "1"}},
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("InitProviderCatalog: %v", err)
	}
	t.Cleanup(func() {
		_ = InitProviderCatalog(&config.AppConfig{})
	})

	refreshTestLoadedModels(t, 0, 24,
		testLLMRow("Reserved/LLM", "bfloat16", nil, 8, 1, 1),
		testLLMRow("keep/llm", "bfloat16", nil, 8, 1, 1),
		testImageRow("reserved/llm", "float16", nil, nil, nil, 8, 1, 1),
		testImageRow("keep/image", "float16", nil, nil, nil, 8, 1, 1),
		testVideoRow("reserved/llm", "bfloat16", nil, nil, nil, 8, 1, 1),
	)

	llmModels := ListLoadedLLMModels()
	if len(llmModels) != 1 || llmModels[0].ModelID != "keep/llm" {
		t.Fatalf("LLM models = %+v", llmModels)
	}
	if _, ok := GetLoadedLLMModel("reserved/llm"); ok {
		t.Fatal("provider-reserved LLM name must not enter Crynux cache")
	}
	imageModels := ListLoadedSDModels()
	if len(imageModels) != 1 || imageModels[0].ModelID != "keep/image" {
		t.Fatalf("image models = %+v", imageModels)
	}
	if _, ok := GetLoadedSDModel("reserved/llm"); ok {
		t.Fatal("provider-reserved image name must not enter Crynux cache")
	}
	if _, ok := GetLoadedVideoModel("reserved/llm"); ok {
		t.Fatal("provider-reserved video name must not enter Crynux cache")
	}
}

func TestInvalidLoadedModelRowsAreSkipped(t *testing.T) {
	autoDType := testLLMRow("org/llm", "auto", nil, 8, 1, 1)
	emptyDType := testLLMRow("org/llm", "", nil, 8, 1, 1)
	emptyModel := testLLMRow("", "bfloat16", nil, 8, 1, 1)
	incompleteComponent := testImageRow("org/image", "bfloat16", nil,
		map[string]any{"transformer": testComponent("", "gguf", nil)}, nil, 8, 1, 1)
	badLoras := testVideoRow("org/video", "bfloat16", nil, nil, []any{map[string]any{"weight": 50}}, 8, 1, 1)
	good := testLLMRow("org/good", "BFloat16", nil, 8, 1, 1)

	refreshTestLoadedModels(t, 0, 24,
		autoDType, emptyDType, emptyModel, incompleteComponent, badLoras, good,
	)

	llmModels := ListLoadedLLMModels()
	if len(llmModels) != 1 || llmModels[0].ModelID != "org/good" || llmModels[0].DType != "bfloat16" {
		t.Fatalf("LLM models = %+v", llmModels)
	}
	if models := ListLoadedSDModels(); len(models) != 0 {
		t.Fatalf("image models = %+v", models)
	}
	if models := ListLoadedVideoModels(); len(models) != 0 {
		t.Fatalf("video models = %+v", models)
	}
}
