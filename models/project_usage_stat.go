package models

import "time"

type ProjectUsageStat struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	CreatedAt        time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"not null"`
	ProjectID        uint      `json:"project_id" gorm:"not null;uniqueIndex:idx_project_usage_stats_project_period"`
	PeriodStart      time.Time `json:"period_start" gorm:"not null;uniqueIndex:idx_project_usage_stats_project_period;index"`
	CallCount        uint64    `json:"call_count" gorm:"not null;default:0"`
	SuccessCount     uint64    `json:"success_count" gorm:"not null;default:0"`
	FailureCount     uint64    `json:"failure_count" gorm:"not null;default:0"`
	PromptTokens     uint64    `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens uint64    `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      uint64    `json:"total_tokens" gorm:"not null;default:0"`
	Credits          BigInt    `json:"credits" gorm:"type:string;size:255;not null"`
}
