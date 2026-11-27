package raw_task

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/relay"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testVideoSchemaURL = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/heads/main/schema/video-task.json"

func setupRawPrepareEnv(t *testing.T, loadedRows []map[string]any) {
	t.Helper()
	cfg := &config.AppConfig{}
	cfg.TaskSchema.GPTInference = "https://raw.githubusercontent.com/crynux-network/gpt-task/refs/tags/v3.7.0/schema/gpt-inference-task.json"
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/stable-diffusion-finetune-lora-task.json"
	cfg.TaskSchema.VideoInference = testVideoSchemaURL
	cfg.LLM.DefaultVramLimit = 24
	cfg.LLM.MinCatalogOnDiskNodeCount = 1
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.BaseVRAM = 8
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.DefaultAutoMaxCreditsPerTask = "100"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "1000"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "0"}
	cfg.LLM.EmptyQueueMedianPriorityGwei = "1"
	cfg.LLM.MaxCrynuxTaskArgsBytes = 1000000
	cfg.LLM.DefaultMaxTokens = 256
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
		case strings.Contains(r.URL.Path, "/v2/models/video/execution-time"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "success",
				"data": map[string]any{
					"overhead_seconds":       1.0,
					"seconds_per_video_unit": 0.0000001,
				},
			})
		case strings.Contains(r.URL.Path, "/v2/models/llm/execution-time"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "success",
				"data": map[string]any{
					"constant_seconds":         1.0,
					"seconds_per_input_token":  0.001,
					"seconds_per_output_token": 0.01,
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

func testVideoLoadedRow(model, dtype string, minVRAM, onDisk int) map[string]any {
	return map[string]any{
		"model_id":             model,
		"model_type":           "video",
		"dtype":                dtype,
		"quantization_config":  nil,
		"components":           map[string]any{},
		"loras":                []any{},
		"min_vram":             minVRAM,
		"in_memory_node_count": 1,
		"on_disk_node_count":   onDisk,
	}
}

func testLLMLoadedRow(model, dtype string, minVRAM int) map[string]any {
	return map[string]any{
		"model_id":             model,
		"model_type":           "llm",
		"dtype":                dtype,
		"quantization_config":  nil,
		"min_vram":             minVRAM,
		"in_memory_node_count": 1,
		"on_disk_node_count":   2,
	}
}

func decodePreparedTaskArgs(t *testing.T, prepared *preparedTask) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(prepared.taskArgs), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestPrepareTaskRejectsUnsupportedTaskType(t *testing.T) {
	for _, value := range []models.TaskType{2, 4} {
		taskType := value
		_, err := prepareTask(context.Background(), &models.Project{}, CreateTaskRequest{
			TaskArgs: `{"version":"3.7.1","model":"example/model","messages":[]}`,
			TaskType: &taskType,
		})
		if err == nil || err.Error() != "task_type must be 0, 1, or 3" {
			t.Fatalf("task_type %d: error = %v", value, err)
		}
	}
}

func TestPrepareTaskLLMRejectsAutoDType(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{testLLMLoadedRow("org/llm", "float16", 20)})
	taskType := models.TaskTypeLLM
	_, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{"version":"3.7.0","model":"org/llm","messages":[{"role":"user","content":"hi"}],"dtype":"auto"}`,
	})
	if !errors.Is(err, service.ErrInvalidExecutionConfig) {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareTaskLLMOmittedDTypeUsesLoadedRow(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{testLLMLoadedRow("org/llm", "float16", 20)})
	taskType := models.TaskTypeLLM
	prepared, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{"version":"3.7.0","model":"Org/LLM","messages":[{"role":"user","content":"hi"}]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodePreparedTaskArgs(t, prepared)
	if payload["dtype"] != "float16" || payload["version"] != "3.7.0" {
		t.Fatalf("dtype/version = %#v %#v", payload["dtype"], payload["version"])
	}
	if prepared.model != "org/llm" || prepared.minVram == nil || *prepared.minVram != 20 {
		t.Fatalf("model=%q minVram=%#v", prepared.model, prepared.minVram)
	}
	if prepared.llmBilling == nil {
		t.Fatal("missing LLM billing data")
	}
}

func TestPrepareTaskVideoResolvesDTypeAndFee(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{testVideoLoadedRow("org/video", "float16", 32, 2)})
	taskType := models.TaskTypeVideo
	prepared, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{
			"version": "3.8.0",
			"base_model": {"name": "Org/Video"},
			"prompt": "a cat",
			"task_config": {"width": 832, "height": 480, "num_frames": 33, "steps": 10}
		}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodePreparedTaskArgs(t, prepared)
	if payload["dtype"] != "float16" || payload["version"] != "3.8.0" {
		t.Fatalf("dtype/version = %#v %#v", payload["dtype"], payload["version"])
	}
	if prepared.model != "org/video" || prepared.minVram == nil || *prepared.minVram != 32 {
		t.Fatalf("model=%q minVram=%#v", prepared.model, prepared.minVram)
	}
	if prepared.llmBilling != nil || prepared.credits == "" || prepared.credits == "0" {
		t.Fatalf("prepared billing = %+v", prepared)
	}
	if !rawTaskModelReady(prepared) {
		t.Fatal("video model with on-disk nodes must be ready")
	}
}

func TestPrepareTaskVideoRejectsAutoDType(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{testVideoLoadedRow("org/video", "float16", 32, 2)})
	taskType := models.TaskTypeVideo
	_, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{"version":"3.8.0","base_model":{"name":"org/video"},"prompt":"a cat","dtype":"auto"}`,
	})
	if !errors.Is(err, service.ErrInvalidExecutionConfig) {
		t.Fatalf("error = %v", err)
	}
}

func testImageLoadedRow(model, dtype string, quant any, components map[string]any, minVRAM int) map[string]any {
	return testImageLoadedRowWithOnDisk(model, dtype, quant, components, minVRAM, 2)
}

func testImageLoadedRowWithOnDisk(model, dtype string, quant any, components map[string]any, minVRAM, onDisk int) map[string]any {
	if components == nil {
		components = map[string]any{}
	}
	return map[string]any{
		"model_id":             model,
		"model_type":           "sd",
		"dtype":                dtype,
		"quantization_config":  quant,
		"components":           components,
		"lora":                 nil,
		"min_vram":             minVRAM,
		"in_memory_node_count": 1,
		"on_disk_node_count":   onDisk,
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

func TestPrepareTaskRejectsRequiredGPU(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.TaskSchema.GPTInference = "https://raw.githubusercontent.com/crynux-network/gpt-task/refs/tags/v3.7.0/schema/gpt-inference-task.json"
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/stable-diffusion-finetune-lora-task.json"
	cfg.TaskSchema.VideoInference = testVideoSchemaURL
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		models.ResetTaskArgsSchemasForTest()
	})

	taskType := models.TaskTypeLLM
	cases := []CreateTaskRequest{
		{
			TaskArgs:    `{"version":"3.7.1","model":"example/model","messages":[]}`,
			TaskType:    &taskType,
			RequiredGPU: "GPU",
		},
		{
			TaskArgs:        `{"version":"3.7.1","model":"example/model","messages":[]}`,
			TaskType:        &taskType,
			RequiredGPUVram: 24,
		},
		{
			TaskArgs:        `{"version":"3.7.1","model":"example/model","messages":[]}`,
			TaskType:        &taskType,
			RequiredGPU:     "GPU",
			RequiredGPUVram: 24,
		},
	}
	for i, req := range cases {
		_, err := prepareTask(context.Background(), &models.Project{}, req)
		if err == nil || !strings.Contains(err.Error(), "required_gpu") {
			t.Fatalf("case %d: error = %v", i, err)
		}
	}
}

func TestParseTaskArgs(t *testing.T) {
	model, dtype, quant, maxTokens, err := parseTaskArgs(
		models.TaskTypeLLM,
		`{"model":"Example/LLM","dtype":"float16","quantization_config":{"quant_method":"awq","quant_kwargs":{}},"generation_config":{"max_new_tokens":64}}`,
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
		`{"base_model":{"name":"Example/Image"},"dtype":"bfloat16"}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if model != "example/image" || dtype != "bfloat16" || quant != nil {
		t.Fatalf("parsed image args = %q %q %#v", model, dtype, quant)
	}
}

func testRawImageProject() *models.Project {
	autoMaxCredits := models.BigInt{Int: *big.NewInt(1_000_000_000)}
	return &models.Project{
		PriorityGwei:          models.BigInt{Int: *big.NewInt(10)},
		AutoMaxCreditsPerTask: &autoMaxCredits,
	}
}

func TestPrepareTaskImageAutoMissRejects(t *testing.T) {
	setupRawPrepareEnv(t, nil)
	taskType := models.TaskTypeImage
	_, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{
			"version": "3.7.1",
			"base_model": {"name": "org/missing"},
			"prompt": "a cat",
			"dtype": "auto",
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
		}`,
	})
	if !errors.Is(err, service.ErrInvalidExecutionConfig) {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareTaskImageAutoPersistsResolvedTaskArgs(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{
		testImageLoadedRow("org/image", "bfloat16", nil, nil, 18),
	})
	taskType := models.TaskTypeImage
	prepared, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
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

func TestPrepareTaskImagePreservesClientTaskArgsVersion(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{
		testImageLoadedRow("org/image", "bfloat16", nil, nil, 18),
	})
	taskType := models.TaskTypeImage
	prepared, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{
			"version": "3.8.0",
			"base_model": {"name": "org/image"},
			"prompt": "a cat",
			"dtype": "bfloat16",
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
	if payload["version"] != "3.8.0" {
		t.Fatalf("version = %#v, want 3.8.0", payload["version"])
	}
}

func TestPrepareTaskRejectsOversizedFinalTaskArgs(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{
		testImageLoadedRow("org/image", "bfloat16", nil, nil, 18),
	})
	config.GetConfig().LLM.MaxCrynuxTaskArgsBytes = 200
	taskType := models.TaskTypeImage
	_, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
		TaskType: &taskType,
		TaskArgs: `{
			"version": "3.7.1",
			"base_model": {"name": "org/image"},
			"prompt": "` + strings.Repeat("a", 150) + `",
			"task_config": {"image_width": 1024, "image_height": 1024, "steps": 40, "num_images_per_prompt": 1}
		}`,
	})
	if err == nil || err.Error() != "task_args size exceeds the 200-byte limit" {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareTaskImageConcreteDtypeAllowsNewModel(t *testing.T) {
	setupRawPrepareEnv(t, []map[string]any{
		testImageLoadedRow("org/image", "bfloat16", nil, nil, 18),
	})
	taskType := models.TaskTypeImage
	prepared, err := prepareTask(context.Background(), testRawImageProject(), CreateTaskRequest{
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
	view := taskView(job, "")
	if view.Status != "running" {
		t.Fatalf("pending status = %q", view.Status)
	}
	if view.Phase != "queued" {
		t.Fatalf("pending phase = %q", view.Phase)
	}
	job.Status = models.TaskJobStatusInProgress
	job.Phase = "validating"
	view = taskView(job, "")
	if view.Status != "running" || view.Phase != "validating" {
		t.Fatalf("in-progress view = %#v", view)
	}
	job.Status = models.TaskJobStatusCompleted
	if got := taskView(job, "").Status; got != "success" {
		t.Fatalf("completed status = %q", got)
	}
	if got := taskView(job, "").Phase; got != "success" {
		t.Fatalf("completed phase = %q", got)
	}
	job.Status = models.TaskJobStatusFailed
	if got := taskView(job, "").Status; got != "failed" {
		t.Fatalf("failed status = %q", got)
	}
	if got := taskView(job, "").Phase; got != "failed" {
		t.Fatalf("failed phase = %q", got)
	}
	if got := taskView(job, "").ErrorMessage; got != "" {
		t.Fatalf("failed without message error_message = %q", got)
	}
	msg := "bridge submit failed: timeout"
	job.ErrorMessage = &msg
	if got := taskView(job, "").ErrorMessage; got != msg {
		t.Fatalf("failed error_message = %q", got)
	}
	job.Status = models.TaskJobStatusCompleted
	if got := taskView(job, "").ErrorMessage; got != "" {
		t.Fatalf("completed error_message = %q", got)
	}
}

func TestTaskViewIncludesPublicDownloadURLsOnSuccess(t *testing.T) {
	hash := "abcd1234efgh5678"
	imageJob := &models.TaskJob{
		Status:             models.TaskJobStatusCompleted,
		TaskType:           models.TaskTypeImage,
		ResultDownloadHash: &hash,
		TaskArgsJSON:       `{"task_config":{"num_images_per_prompt":2,"image_width":512,"image_height":512,"steps":20}}`,
	}
	imageView := taskView(imageJob, "https://as.example")
	if len(imageView.ResultDownloadURLs) != 2 {
		t.Fatalf("image urls = %#v", imageView.ResultDownloadURLs)
	}
	if imageView.ResultDownloadURLs[0] != "https://as.example/api/result/abcd1234efgh5678/images/0" {
		t.Fatalf("image url 0 = %q", imageView.ResultDownloadURLs[0])
	}
	if imageView.ResultDownloadURL != "" {
		t.Fatalf("image view must omit result_download_url, got %q", imageView.ResultDownloadURL)
	}

	videoJob := &models.TaskJob{
		Status:             models.TaskJobStatusCompleted,
		TaskType:           models.TaskTypeVideo,
		ResultDownloadHash: &hash,
	}
	videoView := taskView(videoJob, "https://as.example")
	if videoView.ResultDownloadURL != "https://as.example/api/result/abcd1234efgh5678/video" {
		t.Fatalf("video url = %q", videoView.ResultDownloadURL)
	}
	if len(videoView.ResultDownloadURLs) != 0 {
		t.Fatalf("video view must omit result_download_urls, got %#v", videoView.ResultDownloadURLs)
	}

	running := taskView(&models.TaskJob{
		Status:             models.TaskJobStatusInProgress,
		TaskType:           models.TaskTypeVideo,
		ResultDownloadHash: &hash,
		Phase:              "running",
	}, "https://as.example")
	if running.ResultDownloadURL != "" || len(running.ResultDownloadURLs) != 0 {
		t.Fatalf("running task must omit download urls, got %#v", running)
	}
}

func setupRawCreateRejectDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:raw_create_reject_" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.CreditAccount{},
		&models.Project{},
		&models.TaskJob{},
		&models.TaskCallRecord{},
		&models.UsageStatsPendingCallRecord{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	config.SetDBForTest(db)
	t.Cleanup(func() { config.SetDBForTest(nil) })
	return db
}

func TestCreateTaskModelNotReadyReturns400WithHeldJob(t *testing.T) {
	db := setupRawCreateRejectDB(t)
	cfg := &config.AppConfig{}
	cfg.TaskSchema.GPTInference = "https://raw.githubusercontent.com/crynux-network/gpt-task/refs/tags/v3.7.0/schema/gpt-inference-task.json"
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/stable-diffusion-finetune-lora-task.json"
	cfg.TaskSchema.VideoInference = testVideoSchemaURL
	cfg.LLM.DefaultVramLimit = 24
	cfg.LLM.MinCatalogOnDiskNodeCount = 1
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.BaseVRAM = 8
	cfg.LLM.MinPriorityGwei = "1"
	cfg.LLM.MaxPriorityGwei = "1000000000"
	cfg.LLM.DefaultAutoMaxCreditsPerTask = "1000000000"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "1000"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "0"}
	cfg.LLM.EmptyQueueMedianPriorityGwei = "1"
	cfg.LLM.MaxCrynuxTaskArgsBytes = 1000000
	cfg.LLM.LoadedModelsRefreshInterval = 1800
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		models.ResetTaskArgsSchemasForTest()
	})

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/v2/loaded-models"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "success",
				"data": []map[string]any{
					testImageLoadedRowWithOnDisk("org/cold-image", "bfloat16", nil, nil, 18, 0),
				},
			})
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

	user := models.User{Address: "0xrawnotready1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1_000_000_000)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	autoMax := models.BigInt{Int: *big.NewInt(1_000_000_000)}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "ep-raw-not-ready",
		APIKeyHash: "h", APIKeyPrefix: "pref",
		PriorityGwei:          models.BigInt{Int: *big.NewInt(1)},
		Status:                models.ProjectStatusActive,
		CreditsDay:            models.BigInt{Int: *big.NewInt(0)},
		CostLevelMode:         models.CostLevelModeStatic,
		AutoMaxCreditsPerTask: &autoMax,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/inference_tasks", func(c *gin.Context) {
		c.Set("llm_project", &project)
		CreateTask(c)
	})

	body := `{"task_type":0,"task_args":"{\"version\":\"3.7.1\",\"base_model\":{\"name\":\"org/cold-image\"},\"prompt\":\"a cat\",\"task_config\":{\"image_width\":512,\"image_height\":512,\"steps\":20,\"num_images_per_prompt\":1}}"}`
	req := httptest.NewRequest(http.MethodPost, "/inference_tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["client_task_id"] == nil {
		t.Fatalf("missing client_task_id: %v", resp)
	}
	errObj, _ := resp["error"].(map[string]any)
	if errObj["code"] != "model_not_ready" {
		t.Fatalf("error=%v", errObj)
	}

	var jobs int64
	if err := db.Model(&models.TaskJob{}).Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("jobs=%d, want 1", jobs)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Sign() <= 0 {
		t.Fatalf("locked=%s, want held credits", account.Locked.String())
	}
}

func TestBatchCreateReturns400WhenAnyItemHasError(t *testing.T) {
	db := setupRawCreateRejectDB(t)
	cfg := &config.AppConfig{}
	cfg.TaskSchema.GPTInference = "https://raw.githubusercontent.com/crynux-network/gpt-task/refs/tags/v3.7.0/schema/gpt-inference-task.json"
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/stable-diffusion-finetune-lora-task.json"
	cfg.TaskSchema.VideoInference = testVideoSchemaURL
	cfg.LLM.LoadedModelsRefreshInterval = 1800
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		models.ResetTaskArgsSchemasForTest()
	})

	user := models.User{Address: "0xrawbatch400"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "ep-raw-batch-400",
		APIKeyHash: "h", APIKeyPrefix: "pref",
		PriorityGwei: models.BigInt{Int: *big.NewInt(1)},
		Status:       models.ProjectStatusActive,
		CreditsDay:   models.BigInt{Int: *big.NewInt(0)},
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/inference_tasks/batch", func(c *gin.Context) {
		c.Set("llm_project", &project)
		BatchCreate(c)
	})

	body := `{"tasks":[{"task_type":1,"task_args":"{\"version\":\"3.7.1\",\"model\":\"example/model\",\"messages\":[]}","required_gpu":"GPU"}]}`
	req := httptest.NewRequest(http.MethodPost, "/inference_tasks/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var payload struct {
		Data []BatchCreateItem `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Error == "" || payload.Data[0].ClientTask != nil {
		t.Fatalf("data=%#v", payload.Data)
	}
}

func TestWriteModelNotReadyErrorIncludesClientTaskID(t *testing.T) {
	cfg := &config.AppConfig{}
	cfg.LLM.LoadedModelsRefreshInterval = 1800
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	writeModelNotReadyError(c, gin.H{"client_task_id": uint(42)})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["client_task_id"] != float64(42) {
		t.Fatalf("client_task_id=%v", body["client_task_id"])
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["code"] != "model_not_ready" {
		t.Fatalf("error=%v", errObj)
	}
}

func TestDownloadVideoReturnsStoredMP4(t *testing.T) {
	db := setupRawCreateRejectDB(t)
	cfg := &config.AppConfig{}
	cfg.LLM.TaskResultDir = t.TempDir()
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	bridgeID := uint(77)
	project := models.Project{UserID: 1, Name: "p", EndpointToken: "ep-video-dl", APIKeyHash: "h", APIKeyPrefix: "pref"}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	other := models.Project{UserID: 2, Name: "o", EndpointToken: "ep-video-other", APIKeyHash: "h2", APIKeyPrefix: "pref2"}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	newJob := func(taskType models.TaskType, status models.TaskJobStatus) models.TaskJob {
		job := models.TaskJob{
			ProjectID: project.ID, UserID: project.UserID, TaskType: taskType, APIType: models.TaskAPITypeRaw,
			Model: "org/video", TaskArgsJSON: "{}", Status: status, BridgeClientTaskID: &bridgeID,
		}
		if err := db.Create(&job).Error; err != nil {
			t.Fatal(err)
		}
		return job
	}
	completed := newJob(models.TaskTypeVideo, models.TaskJobStatusCompleted)
	running := newJob(models.TaskTypeVideo, models.TaskJobStatusInProgress)
	image := newJob(models.TaskTypeImage, models.TaskJobStatusCompleted)
	if err := service.SaveVideoResultMP4(completed.ID, []byte("mp4-body")); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/:project/inference_tasks/:client_task_id/video", func(c *gin.Context) {
		if c.Param("project") == "other" {
			c.Set("llm_project", &other)
		} else {
			c.Set("llm_project", &project)
		}
		DownloadVideo(c)
	})
	get := func(projectKey string, jobID uint) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/"+projectKey+"/inference_tasks/"+strconv.FormatUint(uint64(jobID), 10)+"/video", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get("own", completed.ID)
	if w.Code != http.StatusOK || w.Body.String() != "mp4-body" {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("content-type = %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != "attachment; filename=0.mp4" {
		t.Fatalf("content-disposition = %q", got)
	}
	if w := get("other", completed.ID); w.Code != http.StatusNotFound {
		t.Fatalf("cross-project status=%d", w.Code)
	}
	if w := get("own", running.ID); w.Code != http.StatusConflict {
		t.Fatalf("running status=%d", w.Code)
	}
	if w := get("own", image.ID); w.Code != http.StatusBadRequest {
		t.Fatalf("image job status=%d", w.Code)
	}
}

func TestCreateTaskRejectsRequiredGPUWithoutCallRecord(t *testing.T) {
	db := setupRawCreateRejectDB(t)
	cfg := &config.AppConfig{}
	cfg.TaskSchema.GPTInference = "https://raw.githubusercontent.com/crynux-network/gpt-task/refs/tags/v3.7.0/schema/gpt-inference-task.json"
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.1/schema/stable-diffusion-finetune-lora-task.json"
	cfg.TaskSchema.VideoInference = testVideoSchemaURL
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		models.ResetTaskArgsSchemasForTest()
	})

	user := models.User{Address: "0xrawreject1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "ep-raw-reject",
		APIKeyHash: "h", APIKeyPrefix: "pref",
		PriorityGwei: models.BigInt{Int: *big.NewInt(1)},
		Status:       models.ProjectStatusActive,
		CreditsDay:   models.BigInt{Int: *big.NewInt(0)},
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/inference_tasks", func(c *gin.Context) {
		c.Set("llm_project", &project)
		CreateTask(c)
	})

	body := `{"task_type":1,"task_args":"{\"version\":\"3.7.1\",\"model\":\"example/model\",\"messages\":[]}","required_gpu":"NVIDIA RTX PRO 6000"}`
	req := httptest.NewRequest(http.MethodPost, "/inference_tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var jobs, records, pending int64
	if err := db.Model(&models.TaskJob{}).Count(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.TaskCallRecord{}).Count(&records).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.UsageStatsPendingCallRecord{}).Count(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if jobs != 0 || records != 0 || pending != 0 {
		t.Fatalf("jobs=%d records=%d pending=%d, want all 0", jobs, records, pending)
	}
}
