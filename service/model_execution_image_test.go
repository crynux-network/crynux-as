package service

import (
	"encoding/json"
	"testing"
)

var (
	testQuantTransformer = map[string]any{
		"quant_method":           "bitsandbytes",
		"components_to_quantize": []any{"transformer"},
		"quant_kwargs":           map[string]any{"load_in_4bit": true},
	}
	testQuantTextEncoder = map[string]any{
		"quant_method":           "bitsandbytes",
		"components_to_quantize": []any{"text_encoder"},
		"quant_kwargs":           map[string]any{"load_in_8bit": true},
	}
)

func testTransformerComponents(repository string) map[string]any {
	return map[string]any{
		"transformer": testCacheEntry(repository, "gguf", map[string]any{"filename": "model.gguf"}),
	}
}

func applyImage(t *testing.T, taskArgs string, model string) (map[string]any, uint64) {
	t.Helper()
	out, minVRAM, err := ApplyImageExecutionToTaskArgsJSON(taskArgs, model)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	return payload, minVRAM
}

func transformerRepo(t *testing.T, payload map[string]any) any {
	t.Helper()
	components, ok := payload["components"].(map[string]any)
	if !ok {
		t.Fatalf("components missing: %#v", payload["components"])
	}
	transformer, ok := components["transformer"].(map[string]any)
	if !ok {
		t.Fatalf("transformer missing: %#v", components)
	}
	if _, ok := transformer["repository"]; ok {
		t.Fatalf("component must use repo: %#v", transformer)
	}
	return transformer["repo"]
}

const plainImageTaskArgs = `{
	"base_model": {"name": "org/image"},
	"prompt": "a cat",
	"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
}`

func TestApplyImageExecutionKeepsRowsWithDifferentComponentRepos(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("image-a", "org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("image-b", "org/image", "float16", testQuantTransformer, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
	)

	taskArgsB := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"components": {"transformer": {"format": "gguf", "repo": "Org/Quant-B", "format_kwargs": {"filename": "model.gguf"}}}
	}`
	payload, minVRAM := applyImage(t, taskArgsB, "Org/Image")
	if minVRAM != 16 || payload["dtype"] != "float16" {
		t.Fatalf("row b: minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
	if repo := transformerRepo(t, payload); repo != "Org/Quant-B" {
		t.Fatalf("client component repo rewritten: %#v", repo)
	}
	quant, ok := payload["quantization_config"].(map[string]any)
	if !ok || quant["quant_method"] != "bitsandbytes" {
		t.Fatalf("quantization_config = %#v", payload["quantization_config"])
	}

	taskArgsA := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"components": {"transformer": {"format": "gguf", "repo": "org/quant-a", "format_kwargs": {"filename": "model.gguf"}}}
	}`
	payload, minVRAM = applyImage(t, taskArgsA, "org/image")
	if minVRAM != 20 || payload["dtype"] != "bfloat16" {
		t.Fatalf("row a: minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
}

func TestApplyImageExecutionInjectsFullConfigFromSelectedRow(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("image-a", "org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("image-b", "org/image", "float16", testQuantTextEncoder, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
	)

	payload, minVRAM := applyImage(t, plainImageTaskArgs, "org/image")
	if minVRAM != 20 || payload["dtype"] != "bfloat16" {
		t.Fatalf("minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
	if repo := transformerRepo(t, payload); repo != "org/quant-a" {
		t.Fatalf("components from other row: %#v", repo)
	}
	quant, ok := payload["quantization_config"].(map[string]any)
	if !ok {
		t.Fatalf("quantization_config = %#v", payload["quantization_config"])
	}
	toQuantize, _ := quant["components_to_quantize"].([]any)
	if len(toQuantize) != 1 || toQuantize[0] != "transformer" {
		t.Fatalf("quantization_config from other row: %#v", quant)
	}
}

func TestApplyImageExecutionClientQuantizationSelectsMatchingRow(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("image-a", "org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("image-b", "org/image", "float16", testQuantTextEncoder, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
	)

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"quantization_config": {"quant_method": "bitsandbytes", "components_to_quantize": ["text_encoder"], "quant_kwargs": {"load_in_8bit": true}}
	}`
	payload, minVRAM := applyImage(t, taskArgs, "org/image")
	if minVRAM != 16 || payload["dtype"] != "float16" {
		t.Fatalf("minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
	if repo := transformerRepo(t, payload); repo != "org/quant-b" {
		t.Fatalf("components = %#v", repo)
	}
}

func TestApplyImageExecutionDoesNotBorrowFromOtherIdentity(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("image-a", "org/image", "bfloat16", nil, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
	)

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"components": {"transformer": {"format": "gguf", "repo": "org/unknown", "format_kwargs": {"filename": "model.gguf"}}}
	}`
	payload, minVRAM := applyImage(t, taskArgs, "org/image")
	if minVRAM != 24 {
		t.Fatalf("minVRAM = %d, want default 24", minVRAM)
	}
	if _, ok := payload["dtype"]; ok {
		t.Fatalf("dtype borrowed from another row: %#v", payload["dtype"])
	}
	if repo := transformerRepo(t, payload); repo != "org/unknown" {
		t.Fatalf("components = %#v", repo)
	}
	if quant, ok := payload["quantization_config"]; !ok || quant != nil {
		t.Fatalf("quantization_config = %#v", payload["quantization_config"])
	}
}

func TestApplyImageExecutionPlainRequestSkipsLoraRows(t *testing.T) {
	lora := map[string]any{"model": "org/lora", "weight_file_name": "", "weight": 100}
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("image-plain", "org/image", "float16", nil, nil, nil, 12, 1, 2),
		testImageRow("image-lora", "org/image", "bfloat16", nil, nil, lora, 40, 1, 2),
	)

	payload, minVRAM := applyImage(t, plainImageTaskArgs, "org/image")
	if minVRAM != 12 || payload["dtype"] != "float16" {
		t.Fatalf("minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
	if _, ok := payload["lora"]; ok {
		t.Fatalf("lora injected into plain request: %#v", payload["lora"])
	}

	loraArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"lora": {"model": "org/lora"}
	}`
	payload, minVRAM = applyImage(t, loraArgs, "org/image")
	if minVRAM != 40 || payload["dtype"] != "bfloat16" {
		t.Fatalf("lora request: minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
}

func TestGetLoadedSDModelExactUsesFullIdentity(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("image-a", "org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("image-b", "org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
	)

	build := func(repo, dtype string) string {
		return `{
			"base_model": {"name": "org/image"},
			"prompt": "a cat",
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
			"dtype": "` + dtype + `",
			"quantization_config": {"quant_method": "bitsandbytes", "components_to_quantize": ["transformer"], "quant_kwargs": {"load_in_4bit": true}},
			"components": {"transformer": {"format": "gguf", "repo": "` + repo + `", "format_kwargs": {"filename": "model.gguf"}}}
		}`
	}

	loaded, ok, err := GetLoadedSDModelExact("org/image", build("org/quant-b", "bfloat16"))
	if err != nil || !ok || loaded.MinVRAM != 16 {
		t.Fatalf("exact row b = %+v %v %v", loaded, ok, err)
	}
	loaded, ok, err = GetLoadedSDModelExact("org/image", build("org/quant-a", "bfloat16"))
	if err != nil || !ok || loaded.MinVRAM != 20 {
		t.Fatalf("exact row a = %+v %v %v", loaded, ok, err)
	}
	if _, ok, err = GetLoadedSDModelExact("org/image", build("org/quant-c", "bfloat16")); err != nil || ok {
		t.Fatalf("unknown component repo matched: %v %v", ok, err)
	}
	if _, ok, err = GetLoadedSDModelExact("org/image", build("org/quant-a", "float16")); err != nil || ok {
		t.Fatalf("different dtype matched: %v %v", ok, err)
	}
	if _, ok, err = GetLoadedSDModelExact("org/image", plainImageTaskArgs); err != nil || ok {
		t.Fatalf("plain request matched a components row: %v %v", ok, err)
	}
}
