package raw_task

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"strings"
	"testing"
)

func TestPrepareTaskRejectsClientTaskFee(t *testing.T) {
	taskType := models.TaskTypeLLM
	_, err := prepareTask(context.Background(), &models.Project{}, CreateTaskRequest{
		TaskArgs: `{"version":"3.7.0","model":"example/model","messages":[]}`,
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
	cfg.TaskSchema.StableDiffusionInference = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.0/schema/image-task.json"
	cfg.TaskSchema.StableDiffusionFinetuneLora = "https://raw.githubusercontent.com/crynux-network/stable-diffusion-task/refs/tags/v3.7.0/schema/stable-diffusion-finetune-lora-task.json"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() {
		config.SetConfigForTest(nil)
		models.ResetTaskArgsSchemasForTest()
	})

	taskType := models.TaskTypeLLM
	minVram := uint64(24)
	_, err := prepareTask(context.Background(), &models.Project{}, CreateTaskRequest{
		TaskArgs:    `{"version":"3.7.0","model":"example/model","messages":[]}`,
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
