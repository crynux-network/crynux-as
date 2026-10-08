package raw_task

import (
	"context"
	"crynux_as/models"
	"strings"
	"testing"
)

func TestPrepareTaskRejectsClientTaskFee(t *testing.T) {
	taskType := models.TaskTypeLLM
	_, err := prepareTask(context.Background(), &models.Project{}, CreateTaskRequest{
		TaskArgs: `{"model":"example/model"}`,
		TaskType: &taskType,
		TaskFee:  []byte("null"),
	})
	if err == nil || !strings.Contains(err.Error(), "task_fee") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareTaskValidatesHardwareSelection(t *testing.T) {
	taskType := models.TaskTypeLLM
	minVram := uint64(24)
	_, err := prepareTask(context.Background(), &models.Project{}, CreateTaskRequest{
		TaskArgs:    `{"model":"example/model"}`,
		TaskType:    &taskType,
		MinVram:     &minVram,
		RequiredGPU: "GPU",
	})
	if err == nil || !strings.Contains(err.Error(), "min_vram") {
		t.Fatalf("error = %v", err)
	}
}

func TestParseTaskArgs(t *testing.T) {
	model, _, dtype, bits, maxTokens, err := parseTaskArgs(
		models.TaskTypeLLM,
		`{"model":"example/llm","dtype":"float16","quantize_bits":8,"generation_config":{"max_new_tokens":64}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if model != "example/llm" || dtype != "float16" || bits == nil || *bits != 8 || maxTokens != 64 {
		t.Fatalf("parsed LLM args = %q %q %v %d", model, dtype, bits, maxTokens)
	}

	model, variant, _, _, _, err := parseTaskArgs(
		models.TaskTypeImage,
		`{"base_model":{"name":"example/image","variant":"fp16"}}`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if model != "example/image" || variant != "fp16" {
		t.Fatalf("parsed image args = %q %q", model, variant)
	}
}

func TestTaskViewMapsInternalStatuses(t *testing.T) {
	job := &models.TaskJob{Status: models.TaskJobStatusPendingSubmit}
	if got := taskView(job).Status; got != "running" {
		t.Fatalf("pending status = %q", got)
	}
	job.Status = models.TaskJobStatusCompleted
	if got := taskView(job).Status; got != "success" {
		t.Fatalf("completed status = %q", got)
	}
	job.Status = models.TaskJobStatusFailed
	if got := taskView(job).Status; got != "failed" {
		t.Fatalf("failed status = %q", got)
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
