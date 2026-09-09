package models

import "time"

type LLMCallStatus int8

const (
	LLMCallStatusSuccess LLMCallStatus = iota
	LLMCallStatusFailed
)

type LLMCallRecord struct {
	ID                   uint          `json:"id" gorm:"primarykey"`
	CreatedAt            time.Time     `json:"created_at" gorm:"not null;index"`
	UserID               uint          `json:"user_id" gorm:"not null;index"`
	ProjectID            uint          `json:"project_id" gorm:"not null;index"`
	LLMJobID             *uint         `json:"llm_job_id" gorm:"uniqueIndex"`
	Model                string        `json:"model" gorm:"type:string;size:255;not null"`
	PromptTokens         uint64        `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens     uint64        `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens          uint64        `json:"total_tokens" gorm:"not null;default:0"`
	TokenRatio           uint          `json:"token_ratio" gorm:"not null;default:0"`
	TokenUsageApplicable int8          `json:"token_usage_applicable" gorm:"not null"`
	Status               LLMCallStatus `json:"status" gorm:"not null;default:0;index"`
	Credits              BigInt        `json:"credits" gorm:"type:string;size:255;not null"`
	AcceptedAt           time.Time     `json:"accepted_at" gorm:"not null;index"`
	CompletedAt          time.Time     `json:"completed_at" gorm:"not null;index"`
	DurationMs           uint64        `json:"duration_ms" gorm:"not null;default:0"`
	BilledVram           uint64        `json:"billed_vram" gorm:"not null;default:0"`
	TaskFeeGwei          *BigInt       `json:"task_fee_gwei" gorm:"type:string;size:255"`
	MedianPriorityGwei   *BigInt       `json:"median_priority_gwei" gorm:"type:string;size:255"`
	EstimatedNodeSeconds *float64      `json:"estimated_node_seconds"`
	VramWeight           *float64      `json:"vram_weight"`
}
