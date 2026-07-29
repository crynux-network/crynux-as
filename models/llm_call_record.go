package models

import "time"

type LLMCallStatus int8

const (
	LLMCallStatusSuccess LLMCallStatus = iota
	LLMCallStatusFailed
)

type LLMCallRecord struct {
	ID               uint          `json:"id" gorm:"primarykey"`
	CreatedAt        time.Time     `json:"created_at" gorm:"not null;index"`
	ProjectID        uint          `json:"project_id" gorm:"not null;index"`
	Model            string        `json:"model" gorm:"type:string;size:255;not null"`
	PromptTokens     uint64        `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens uint64        `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      uint64        `json:"total_tokens" gorm:"not null;default:0"`
	TokenRatio       uint          `json:"token_ratio" gorm:"not null;default:0"`
	Status           LLMCallStatus `json:"status" gorm:"not null;default:0;index"`
	Credits          BigInt        `json:"credits" gorm:"type:string;size:255;not null"`
	DurationMs       uint64        `json:"duration_ms" gorm:"not null;default:0"`
	BilledVram       uint64        `json:"billed_vram" gorm:"not null;default:0"`
}
