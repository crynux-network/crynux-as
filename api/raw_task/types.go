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
	TaskVersion     *string          `json:"task_version,omitempty"`
	MinVram         *uint64          `json:"min_vram,omitempty"`
	RequiredGPU     string           `json:"required_gpu,omitempty"`
	RequiredGPUVram uint64           `json:"required_gpu_vram,omitempty"`
	RepeatNum       *uint64          `json:"repeat_num,omitempty"`
	TaskFee         json.RawMessage  `json:"task_fee,omitempty"`
}

type TaskView struct {
	ID        uint            `json:"id"`
	TaskType  models.TaskType `json:"task_type"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type BatchCreateRequest struct {
	Tasks []CreateTaskRequest `json:"tasks"`
}

type BatchCreateItem struct {
	Index      int       `json:"index"`
	ClientTask *TaskView `json:"client_task,omitempty"`
	Error      string    `json:"error,omitempty"`
}

type BatchStatusRequest struct {
	ClientTaskIDs []uint `json:"client_task_ids"`
}

type BatchStatusItem struct {
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
	repeatNum  uint64
	priority   string
	taskFeeWei string
	credits    string
	llmBilling *models.LLMTaskBillingData
}
