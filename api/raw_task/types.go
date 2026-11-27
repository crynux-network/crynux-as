package raw_task

import (
	"crynux_as/models"
	"encoding/json"
	"time"
)

const maxBatchSize = 100

type CreateTaskRequest struct {
	TaskArgs        string           `json:"task_args"`
	TaskType        *models.TaskType `json:"task_type"`
	MinVram         *uint64          `json:"min_vram,omitempty"`
	RequiredGPU     string           `json:"required_gpu,omitempty"`
	RequiredGPUVram uint64           `json:"required_gpu_vram,omitempty"`
	TaskFee         json.RawMessage  `json:"task_fee,omitempty"`
	QueueTimeout    *uint64          `json:"queue_timeout,omitempty"`
}

type TaskView struct {
	ID                    uint            `json:"id"`
	TaskType              models.TaskType `json:"task_type"`
	Status                string          `json:"status"`
	Phase                 string          `json:"phase"`
	EstimatedCompletionAt *time.Time      `json:"estimated_completion_at,omitempty"`
	ErrorMessage          string          `json:"error_message,omitempty"`
	ResultDownloadHash    string          `json:"result_download_hash,omitempty"`
	// ResultDownloadURLs is set for successful Image tasks: one public URL per image index.
	ResultDownloadURLs []string `json:"result_download_urls,omitempty"`
	// ResultDownloadURL is set for a successful Video task: the public MP4 URL.
	ResultDownloadURL string    `json:"result_download_url,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type BatchCreateRequest struct {
	Tasks []CreateTaskRequest `json:"tasks"`
}

type BatchCreateItem struct {
	Index        int       `json:"index"`
	ClientTask   *TaskView `json:"client_task,omitempty"`
	Error        string    `json:"error,omitempty"`
	ClientTaskID *uint     `json:"client_task_id,omitempty"`
}

type BatchStatusRequest struct {
	ClientTaskIDs []uint `json:"client_task_ids"`
}

type BatchStatusItem struct {
	ClientTaskID uint      `json:"client_task_id"`
	ClientTask   *TaskView `json:"client_task,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type BatchAbortRequest struct {
	ClientTaskIDs []uint `json:"client_task_ids"`
}

type BatchAbortItem struct {
	ClientTaskID uint      `json:"client_task_id"`
	ClientTask   *TaskView `json:"client_task,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type preparedTask struct {
	request    CreateTaskRequest
	model      string
	taskArgs   string
	minVram    *uint64
	billedVram uint64
	priority   string
	taskFeeWei string
	credits    string
	llmBilling *models.LLMTaskBillingData
}
