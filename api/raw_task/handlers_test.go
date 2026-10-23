package raw_task

import (
	"context"
	"crypto/sha256"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/relay"
	"crynux_as/service"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupRawImagePrepareEnv(t *testing.T, loadedRows []map[string]any) {
	t.Helper()
	cfg := &config.AppConfig{}
	cfg.TaskSchema.GPTInference = "https://raw.githubusercontent.com/crynux-network/gpt-task/refs/tags/v3.7.0/schema/gpt-inference-task.json"
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/stable-diffusion-finetune-lora-task.json"
	cfg.LLM.DefaultVramLimit = 24
	cfg.LLM.MinCatalogOnDiskNodeCount = 1
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.BaseVRAM = 8
	cfg.LLM.MaxTaskPriceCNX = "1000"
	cfg.LLM.EmptyQueueMedianPriorityGwei = "1"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		models.ResetTaskArgsSchemasForTest()
	})

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v2/loaded-models"):
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "success", "data": loadedRows})
		case strings.Contains(r.URL.Path, "/v2/models/sd/execution-time"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "success",
				"data": map[string]any{
					"overhead_seconds":          1.0,
					"seconds_per_sd_pixel_step": 0.000001,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(relayServer.Close)

	client := relay.NewClient(relayServer.URL)
	service.InitLoadedModelsCache(client)
	if err := service.RefreshLoadedModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	service.InitExecutionTimeCache(client, 60)
}

func testImageLoadedRow(seed, model, dtype string, quant any, components map[string]any, minVRAM int) map[string]any {
	if components == nil {
		components = map[string]any{}
	}
	sum := sha256.Sum256([]byte(seed))
	return map[string]any{
		"model_id":            model,
		"model_type":          "sd",
		"model_cache_hash":    hex.EncodeToString(sum[:]),
		"execution_dtype":     dtype,
		"quantization_config": quant,
		"model_cache_config": map[string]any{
			"task_kind":           "image",
			"base_model":          map[string]any{"source_type": "huggingface", "repository": model, "format": "diffusers", "format_kwargs": map[string]any{}},
			"components":          components,
			"lora":                nil,
			"dtype":               dtype,
			"quantization_config": quant,
		},
		"min_vram":             minVRAM,
		"in_memory_node_count": 1,
		"on_disk_node_count":   2,
	}
}

func TestPrepareTaskRejectsClientTaskFee(t *testing.T) {
	taskType := models.TaskTypeLLM
	_, err := prepareTask(context.Background(), &models.Project{}, CreateTaskRequest{
		TaskArgs: `{"version":"3.7.1","model":"example/model","messages":[]}`,
		TaskType: &taskType,
		TaskFee:  []byte("null"),
	})
	if err == nil || !strings.Contains(err.Error(), "task_fee") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareTaskValidatesHardwareSelection(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.TaskSchema.GPTInference = "https://raw.githubusercontent.com/crynux-network/gpt-task/refs/tags/v3.7.0/schema/gpt-inference-task.json"
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/stable-diffusion-finetune-lora-task.json"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		models.ResetTaskArgsSchemasForTest()
	})

	taskType := models.TaskTypeLLM
	minVram := uint64(24)
	_, err := prepareTask(context.Background(), &models.Project{}, CreateTaskRequest{
		TaskArgs:    `{"version":"3.7.1","model":"example/model","messages":[]}`,
		TaskType:    &taskType,
		MinVram:     &minVram,
		RequiredGPU: "GPU",
	})
	if err == nil || !strings.Contains(err.Error(), "min_vram") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseTaskArgs(t *testing.T) {
	model, dtype, quant, maxTokens, err := parseTaskArgs(
		models.TaskTypeLLM,
		`{"model":"example/llm","dtype":"float16","quantization_config":{"quant_method":"awq","quant_kwargs":{}},"generation_config":{"max_new_tokens":64}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	quantMap, ok := quant.(map[string]any)
	if !ok || model != "example/llm" || dtype != "float16" || maxTokens != 64 {
		t.Fatalf("parsed LLM args = %q %q %#v %d", model, dtype, quant, maxTokens)
	}
	if quantMap["quant_method"] != "awq" {
		t.Fatalf("quant_method = %#v", quantMap["quant_method"])
	}

	model, dtype, quant, _, err = parseTaskArgs(
		models.TaskTypeImage,
		`{"base_model":{"name":"example/image"},"dtype":"bfloat16"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if model != "example/image" || dtype != "bfloat16" || quant != nil {
		t.Fatalf("parsed image args = %q %q %#v", model, dtype, quant)
	}
}

func TestPrepareTaskImageAutoMissRejects(t *testing.T) {
	setupRawImagePrepareEnv(t, nil)
	taskType := models.TaskTypeImage
	_, err := prepareTask(context.Background(), &models.Project{
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
	}, CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{
			"version": "3.7.1",
			"base_model": {"name": "org/missing"},
			"prompt": "a cat",
			"dtype": "auto",
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
		}`,
	})
	if !errors.Is(err, service.ErrImageExecutionUnresolved) {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareTaskImageAutoPersistsResolvedTaskArgs(t *testing.T) {
	setupRawImagePrepareEnv(t, []map[string]any{
		testImageLoadedRow("plain", "org/image", "bfloat16", nil, nil, 18),
	})
	taskType := models.TaskTypeImage
	prepared, err := prepareTask(context.Background(), &models.Project{
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
	}, CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{
			"version": "3.7.1",
			"base_model": {"name": "org/image"},
			"prompt": "a cat",
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
		}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(prepared.taskArgs), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["dtype"] != "bfloat16" {
		t.Fatalf("dtype = %#v", payload["dtype"])
	}
	if prepared.minVram == nil || *prepared.minVram != 18 {
		t.Fatalf("minVram = %#v", prepared.minVram)
	}
}

func TestPrepareTaskImageConcreteDtypeAllowsNewModel(t *testing.T) {
	setupRawImagePrepareEnv(t, []map[string]any{
		testImageLoadedRow("other", "org/image", "bfloat16", nil, nil, 18),
	})
	taskType := models.TaskTypeImage
	prepared, err := prepareTask(context.Background(), &models.Project{
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
	}, CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{
			"version": "3.7.1",
			"base_model": {"name": "org/image"},
			"prompt": "a cat",
			"dtype": "float16",
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1},
			"components": {"transformer": {"format": "gguf", "repo": "org/new-quant", "format_kwargs": {"filename": "model.gguf"}}}
		}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(prepared.taskArgs), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["dtype"] != "float16" {
		t.Fatalf("dtype = %#v", payload["dtype"])
	}
	if prepared.minVram == nil || *prepared.minVram != 24 {
		t.Fatalf("minVram = %#v, want default 24", prepared.minVram)
	}
}

func TestTaskViewMapsInternalStatuses(t *testing.T) {
	job := &models.TaskJob{Status: models.TaskJobStatusPendingSubmit}
	view := taskView(job)
	if view.Status != "running" {
		t.Fatalf("pending status = %q", view.Status)
	}
	if view.Phase != "queued" {
		t.Fatalf("pending phase = %q", view.Phase)
	}
	job.Status = models.TaskJobStatusInProgress
	job.Phase = "validating"
	view = taskView(job)
	if view.Status != "running" || view.Phase != "validating" {
		t.Fatalf("in-progress view = %#v", view)
	}
	job.Status = models.TaskJobStatusCompleted
	if got := taskView(job).Status; got != "success" {
		t.Fatalf("completed status = %q", got)
	}
	if got := taskView(job).Phase; got != "success" {
		t.Fatalf("completed phase = %q", got)
	}
	job.Status = models.TaskJobStatusFailed
	if got := taskView(job).Status; got != "failed" {
		t.Fatalf("failed status = %q", got)
	}
	if got := taskView(job).Phase; got != "failed" {
		t.Fatalf("failed phase = %q", got)
	}
	if got := taskView(job).ErrorMessage; got != "" {
		t.Fatalf("failed without message error_message = %q", got)
	}
	msg := "bridge submit failed: timeout"
	job.ErrorMessage = &msg
	if got := taskView(job).ErrorMessage; got != msg {
		t.Fatalf("failed error_message = %q", got)
	}
	job.Status = models.TaskJobStatusCompleted
	if got := taskView(job).ErrorMessage; got != "" {
		t.Fatalf("completed error_message = %q", got)
	}
}
