package models

import "time"

type TaskCallStatus int8

const (
	TaskCallStatusSuccess TaskCallStatus = iota
	TaskCallStatusFailed
)

type TaskCallRecord struct {
	ID                    uint           `json:"id" gorm:"primarykey"`
	CreatedAt             time.Time      `json:"created_at" gorm:"not null;index:idx_task_call_records_project_created,priority:2;index"`
	UserID                uint           `json:"user_id" gorm:"not null;index"`
	ProjectID             uint           `json:"project_id" gorm:"not null;index:idx_task_call_records_project_created,priority:1;index"`
	TaskJobID             *uint          `json:"task_job_id" gorm:"uniqueIndex"`
	TaskType              TaskType       `json:"task_type" gorm:"not null"`
	APIType               TaskAPIType    `json:"api_type" gorm:"not null"`
	Model                 string         `json:"model" gorm:"type:string;size:255;not null"`
	PromptTokens          uint64         `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens      uint64         `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens           uint64         `json:"total_tokens" gorm:"not null;default:0"`
	PriorityGwei          BigInt         `json:"priority_gwei" gorm:"type:string;size:255;not null"`
	TokenUsageApplicable  int8           `json:"token_usage_applicable" gorm:"not null"`
	Status                TaskCallStatus `json:"status" gorm:"not null;default:0;index"`
	AcceptedAt            time.Time      `json:"accepted_at" gorm:"not null;index"`
	CompletedAt           time.Time      `json:"completed_at" gorm:"not null;index"`
	DurationMs            uint64         `json:"duration_ms" gorm:"not null;default:0"`
	BilledVram            uint64         `json:"billed_vram" gorm:"not null;default:0"`
	TaskFeeGwei           *BigInt        `json:"-" gorm:"-"`
	MedianPriorityGwei    *BigInt        `json:"-" gorm:"-"`
	EstimatedNodeSeconds  *float64       `json:"-" gorm:"-"`
	VramWeight            *float64       `json:"-" gorm:"-"`
	ConstantSeconds       *float64       `json:"-" gorm:"-"`
	SecondsPerInputToken  *float64       `json:"-" gorm:"-"`
	SecondsPerOutputToken *float64       `json:"-" gorm:"-"`
	CreditsPerGwei        *string        `json:"-" gorm:"-"`
}
