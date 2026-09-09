package models

import (
	"time"
)

type LLMAPIType int8

const (
	LLMAPITypeResponses LLMAPIType = iota
	LLMAPITypeChatCompletions
	LLMAPITypeCompletions
)

type LLMJobStatus int8

const (
	LLMJobStatusPendingSubmit LLMJobStatus = iota
	LLMJobStatusSubmitted
	LLMJobStatusInProgress
	LLMJobStatusCompleted
	LLMJobStatusFailed
)

type LLMJobBillingStatus int8

const (
	LLMJobBillingPending LLMJobBillingStatus = iota
	LLMJobBillingBilled
	LLMJobBillingNotBilled
)

type LLMJob struct {
	ID                    uint                `gorm:"primarykey"`
	CreatedAt             time.Time           `gorm:"not null;index"`
	UpdatedAt             time.Time           `gorm:"not null"`
	ProjectID             uint                `gorm:"not null;index"`
	UserID                uint                `gorm:"not null;index"`
	TokenRatio            uint                `gorm:"not null;default:0"`
	PublicID              string              `gorm:"type:string;size:128;index"`
	APIType               LLMAPIType          `gorm:"not null"`
	Model                 string              `gorm:"type:string;size:255;not null"`
	BilledVram            uint64              `gorm:"not null;default:0"`
	Background            bool                `gorm:"not null;default:false"`
	Stream                bool                `gorm:"not null;default:false"`
	RequestBody           []byte              `gorm:"type:longblob"`
	TaskArgsJSON          string              `gorm:"type:longtext;not null"`
	BridgeClientTaskID    *uint               `gorm:"index"`
	Status                LLMJobStatus        `gorm:"not null;default:0;index"`
	RawResultJSON         *string             `gorm:"type:longtext"`
	FormattedResultJSON   *string             `gorm:"type:longtext"`
	PromptTokens          uint64              `gorm:"not null;default:0"`
	CompletionTokens      uint64              `gorm:"not null;default:0"`
	TotalTokens           uint64              `gorm:"not null;default:0"`
	ErrorMessage          *string             `gorm:"type:text"`
	BillingStatus         LLMJobBillingStatus `gorm:"not null;default:0;index"`
	LLMCallRecordID       *uint               `gorm:"index"`
	TaskFeeGwei           *BigInt             `gorm:"type:string;size:255"`
	MedianPriorityGwei    *BigInt             `gorm:"type:string;size:255"`
	EstimatedNodeSeconds  *float64
	VramWeight            *float64
	ConstantSeconds       *float64
	SecondsPerInputToken  *float64
	SecondsPerOutputToken *float64
	StartedAt             *time.Time
	CompletedAt           *time.Time
}

func (LLMJob) TableName() string {
	return "llm_jobs"
}

func (j LLMJob) IsTerminal() bool {
	return j.Status == LLMJobStatusCompleted || j.Status == LLMJobStatusFailed
}
