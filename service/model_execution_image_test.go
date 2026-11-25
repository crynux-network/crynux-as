package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func assertLLMInt8ThresholdFloatLiteral(t *testing.T, taskArgsJSON string) {
	t.Helper()
	if !strings.Contains(taskArgsJSON, `"llm_int8_threshold":6.0`) {
		t.Fatalf("missing llm_int8_threshold 6.0 literal in %s", taskArgsJSON)
	}
	if strings.Contains(taskArgsJSON, `"llm_int8_threshold":6,`) ||
		strings.Contains(taskArgsJSON, `"llm_int8_threshold":6}`) {
		t.Fatalf("collapsed llm_int8_threshold integer 6 in %s", taskArgsJSON)
	}
}

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
	testQuantGGUF4Bit = map[string]any{
		"quant_method":           "gguf",
		"components_to_quantize": []any{"transformer"},
		"quant_kwargs":           map[string]any{"quant_type": "Q4_K"},
	}
)

func testTransformerComponents(repository string) map[string]any {
	return map[string]any{
		"transformer": testComponent(repository, "gguf", map[string]any{"filename": "model.gguf"}),
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

func applyImageErr(t *testing.T, taskArgs string, model string) error {
	t.Helper()
	_, _, err := ApplyImageExecutionToTaskArgsJSON(taskArgs, model)
	return err
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
		testImageRow("org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("org/image", "float16", testQuantTransformer, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
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
	if repo := transformerRepo(t, payload); repo != "org/quant-b" {
		t.Fatalf("client component repo = %#v", repo)
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
		testImageRow("org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("org/image", "float16", testQuantTextEncoder, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
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
		testImageRow("org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("org/image", "float16", testQuantTextEncoder, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
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

func TestApplyImageExecutionOmittedDtypeMissFallsBackToBFloat16(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "float16", nil, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
	)

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"components": {"transformer": {"format": "gguf", "repo": "org/unknown", "format_kwargs": {"filename": "model.gguf"}}}
	}`
	payload, minVRAM := applyImage(t, taskArgs, "org/image")
	if minVRAM != 24 || payload["dtype"] != "bfloat16" {
		t.Fatalf("minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
	if repo := transformerRepo(t, payload); repo != "org/unknown" {
		t.Fatalf("components borrowed: %#v", repo)
	}
	if _, ok := payload["quantization_config"]; ok {
		t.Fatalf("quantization_config injected on miss: %#v", payload["quantization_config"])
	}
}

func TestApplyImageExecutionPlainRequestSkipsLoraRows(t *testing.T) {
	lora := map[string]any{"model": "org/lora", "weight_file_name": "", "weight": 100}
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "float16", nil, nil, nil, 12, 1, 2),
		testImageRow("org/image", "bfloat16", nil, nil, lora, 40, 1, 2),
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

func TestApplyImageExecutionConcreteDtypeKeepsFieldsOnCacheMiss(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/other", "bfloat16", nil, nil, nil, 20, 1, 2),
	)

	for _, dtype := range []string{"float16", "bfloat16", "float32"} {
		taskArgs := `{
			"base_model": {"name": "org/new-image"},
			"prompt": "a cat",
			"dtype": "` + dtype + `",
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
			"components": {"transformer": {"format": "gguf", "repo": "org/quant-new", "format_kwargs": {"filename": "model.gguf"}}}
		}`
		payload, minVRAM := applyImage(t, taskArgs, "org/new-image")
		if minVRAM != 24 {
			t.Fatalf("dtype %s: minVRAM = %d, want default 24", dtype, minVRAM)
		}
		if payload["dtype"] != dtype {
			t.Fatalf("dtype = %#v, want %s", payload["dtype"], dtype)
		}
		if repo := transformerRepo(t, payload); repo != "org/quant-new" {
			t.Fatalf("components borrowed: %#v", repo)
		}
		if _, ok := payload["quantization_config"]; ok {
			t.Fatalf("quantization_config injected on concrete miss: %#v", payload["quantization_config"])
		}
	}
}

func TestApplyImageExecutionOmittedDtypeInjectsConcreteDtypeFromGGUFRow(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "float16", testQuantGGUF4Bit, testTransformerComponents("org/gguf-4bit"), nil, 18, 1, 2),
	)

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"quantization_config": {"quant_method": "gguf", "components_to_quantize": ["transformer"], "quant_kwargs": {"quant_type": "Q4_K"}},
		"components": {"transformer": {"format": "gguf", "repo": "org/gguf-4bit", "format_kwargs": {"filename": "model.gguf"}}}
	}`
	payload, minVRAM := applyImage(t, taskArgs, "org/image")
	if minVRAM != 18 || payload["dtype"] != "float16" {
		t.Fatalf("minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
}

func TestApplyImageExecutionOmittedDtypeQuantMismatchFallsBack(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "float16", testQuantGGUF4Bit, testTransformerComponents("org/gguf-4bit"), nil, 18, 1, 2),
	)

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"quantization_config": {"quant_method": "gguf", "components_to_quantize": ["transformer"], "quant_kwargs": {"quant_type": "Q8_0"}},
		"components": {"transformer": {"format": "gguf", "repo": "org/gguf-4bit", "format_kwargs": {"filename": "model.gguf"}}}
	}`
	payload, minVRAM := applyImage(t, taskArgs, "org/image")
	if minVRAM != 24 || payload["dtype"] != "bfloat16" {
		t.Fatalf("minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
}

func TestApplyImageExecutionRejectsNonConcreteDtype(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "bfloat16", nil, nil, nil, 12, 1, 2),
	)

	for _, dtype := range []string{`"auto"`, `""`, `null`, `"fp16"`, `16`} {
		taskArgs := `{
			"base_model": {"name": "org/image"},
			"prompt": "a cat",
			"dtype": ` + dtype + `,
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
		}`
		if err := applyImageErr(t, taskArgs, "org/image"); !errors.Is(err, ErrInvalidExecutionConfig) {
			t.Fatalf("dtype %s: error = %v", dtype, err)
		}
	}
}

func TestApplyImageExecutionPlainRequestDoesNotSelectLoraRows(t *testing.T) {
	lora := map[string]any{"model": "org/lora", "weight_file_name": "", "weight": 100}
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "float16", nil, nil, lora, 40, 1, 2),
	)

	payload, minVRAM := applyImage(t, plainImageTaskArgs, "org/image")
	if minVRAM != 24 || payload["dtype"] != "bfloat16" {
		t.Fatalf("minVRAM = %d dtype = %#v", minVRAM, payload["dtype"])
	}
	if _, ok := payload["lora"]; ok {
		t.Fatalf("lora injected into plain request: %#v", payload["lora"])
	}
}

func TestApplyImageExecutionConcreteDtypeUsesExactIdentityMinVRAM(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 20, 1, 2),
		testImageRow("org/image", "bfloat16", testQuantTransformer, testTransformerComponents("org/quant-b"), nil, 16, 1, 2),
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

	cases := []struct {
		taskArgs string
		minVRAM  uint64
	}{
		{build("org/quant-b", "BFloat16"), 16},
		{build("org/quant-a", "bfloat16"), 20},
		{build("org/quant-c", "bfloat16"), 24},
		{build("org/quant-a", "float16"), 24},
	}
	for _, tc := range cases {
		resolved, err := ResolveImageModelExecution(tc.taskArgs, "org/image")
		if err != nil {
			t.Fatal(err)
		}
		if resolved.MinVRAM != tc.minVRAM || resolved.FromLoadedModels != (tc.minVRAM != 24) {
			t.Fatalf("resolved = %+v, want min_vram %d", resolved, tc.minVRAM)
		}
	}
}

const plainVideoTaskArgs = `{
	"version": "3.8.0",
	"base_model": {"name": "Org/Video"},
	"prompt": "a cat",
	"task_config": {"width": 832, "height": 480, "num_frames": 81, "steps": 30}
}`

func TestResolveVideoExecutionMatchesLorasAndFillsFromRow(t *testing.T) {
	loras := []any{map[string]any{"model": "org/lora", "weight_file_name": "", "weight": 100, "target": "auto"}}
	refreshTestLoadedModels(t, 1, 24,
		testVideoRow("org/video", "float16", testQuantTransformer, testTransformerComponents("org/quant-a"), nil, 48, 1, 2),
		testVideoRow("org/video", "bfloat16", nil, nil, loras, 80, 1, 2),
		testImageRow("org/video", "float32", nil, nil, nil, 96, 1, 2),
	)

	resolved, err := ResolveVideoModelExecution(plainVideoTaskArgs, "Org/Video")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(resolved.TaskArgsJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if resolved.MinVRAM != 48 || payload["dtype"] != "float16" || payload["version"] != "3.8.0" {
		t.Fatalf("resolved = %+v payload = %#v", resolved, payload)
	}
	if repo := transformerRepo(t, payload); repo != "org/quant-a" {
		t.Fatalf("components = %#v", repo)
	}

	loraArgs := `{
		"version": "3.8.0",
		"base_model": {"name": "org/video"},
		"prompt": "a cat",
		"loras": [{"model": "Org/LoRA"}]
	}`
	resolved, err = ResolveVideoModelExecution(loraArgs, "org/video")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MinVRAM != 80 || !strings.Contains(resolved.TaskArgsJSON, `"dtype":"bfloat16"`) {
		t.Fatalf("lora request resolved = %+v", resolved)
	}
}

func TestResolveVideoExecutionMissAndConcreteDtype(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testVideoRow("org/video", "float16", nil, nil, nil, 48, 1, 2),
	)

	resolved, err := ResolveVideoModelExecution(plainVideoTaskArgs, "org/new-video")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MinVRAM != 24 || resolved.FromLoadedModels || !strings.Contains(resolved.TaskArgsJSON, `"dtype":"bfloat16"`) {
		t.Fatalf("miss resolved = %+v", resolved)
	}

	concrete := strings.Replace(plainVideoTaskArgs, `"prompt": "a cat",`, `"prompt": "a cat", "dtype": "float16",`, 1)
	resolved, err = ResolveVideoModelExecution(concrete, "org/video")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MinVRAM != 48 || !resolved.FromLoadedModels {
		t.Fatalf("concrete resolved = %+v", resolved)
	}

	auto := strings.Replace(plainVideoTaskArgs, `"prompt": "a cat",`, `"prompt": "a cat", "dtype": "auto",`, 1)
	if _, err := ResolveVideoModelExecution(auto, "org/video"); !errors.Is(err, ErrInvalidExecutionConfig) {
		t.Fatalf("auto dtype error = %v", err)
	}
}

func TestResolveImagePreservesClientInt8ThresholdFloatLiteral(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "bfloat16", nil, nil, nil, 32, 1, 2),
	)

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"dtype": "bfloat16",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
		"quantization_config": {
			"quant_method": "bitsandbytes",
			"components_to_quantize": ["transformer", "text_encoder"],
			"quant_kwargs": {"load_in_8bit": true, "llm_int8_threshold": 6.0}
		}
	}`
	out, _, err := ApplyImageExecutionToTaskArgsJSON(taskArgs, "org/image")
	if err != nil {
		t.Fatal(err)
	}
	assertLLMInt8ThresholdFloatLiteral(t, out)
}

func TestResolveImageInjectsLoadedInt8ThresholdFloatLiteral(t *testing.T) {
	quant := json.RawMessage(`{"quant_method":"bitsandbytes","components_to_quantize":["transformer","text_encoder"],"quant_kwargs":{"load_in_8bit":true,"llm_int8_threshold":6.0}}`)
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "bfloat16", quant, nil, nil, 32, 1, 2),
	)

	taskArgs := `{
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
	}`
	out, minVRAM, err := ApplyImageExecutionToTaskArgsJSON(taskArgs, "org/image")
	if err != nil {
		t.Fatal(err)
	}
	if minVRAM != 32 {
		t.Fatalf("minVRAM = %d", minVRAM)
	}
	assertLLMInt8ThresholdFloatLiteral(t, out)
}

func TestQuantizationConfigForAPIPreservesFloatLiteral(t *testing.T) {
	raw := json.RawMessage(`{"quant_kwargs":{"llm_int8_threshold":6.0,"load_in_8bit":true},"quant_method":"bitsandbytes"}`)
	encoded, err := json.Marshal(map[string]any{"quantization_config": QuantizationConfigForAPI(raw)})
	if err != nil {
		t.Fatal(err)
	}
	assertLLMInt8ThresholdFloatLiteral(t, string(encoded))
}

func TestResolveImagePreservesClientVersionAndDefaultsWhenMissing(t *testing.T) {
	refreshTestLoadedModels(t, 1, 24,
		testImageRow("org/image", "bfloat16", nil, nil, nil, 32, 1, 2),
	)

	withVersion := `{
		"version": "3.8.0",
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"dtype": "bfloat16",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
	}`
	payload, _ := applyImage(t, withVersion, "org/image")
	if payload["version"] != "3.8.0" {
		t.Fatalf("version = %#v, want 3.8.0", payload["version"])
	}

	autoWithVersion := `{
		"version": "3.8.0",
		"base_model": {"name": "org/image"},
		"prompt": "a cat",
		"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
	}`
	payload, _ = applyImage(t, autoWithVersion, "org/image")
	if payload["version"] != "3.8.0" {
		t.Fatalf("auto mode version = %#v, want 3.8.0", payload["version"])
	}

	payload, _ = applyImage(t, plainImageTaskArgs, "org/image")
	if payload["version"] != "3.7.1" {
		t.Fatalf("missing version = %#v, want default 3.7.1", payload["version"])
	}
}

func TestApplyLLMExecutionPreservesInt8ThresholdFloatLiteral(t *testing.T) {
	taskArgs := `{
		"model": "org/llm",
		"messages": [{"role": "user", "content": "hi"}],
		"dtype": "bfloat16",
		"quantization_config": {
			"quant_method": "bitsandbytes",
			"quant_kwargs": {"load_in_8bit": true, "llm_int8_threshold": 6.0}
		}
	}`
	out, err := ApplyLLMExecutionToTaskArgsJSON(taskArgs, ModelExecutionSelection{
		Dtype:              "bfloat16",
		QuantizationConfig: json.RawMessage(`{"quant_method":"bitsandbytes","quant_kwargs":{"load_in_8bit":true,"llm_int8_threshold":6.0}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertLLMInt8ThresholdFloatLiteral(t, out)
}
