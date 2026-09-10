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
	CreatedAt             time.Time           `gorm:"not null;index:idx_llm_jobs_project_status_created,priority:3;index"`
	UpdatedAt             time.Time           `gorm:"not null"`
	ProjectID             uint                `gorm:"not null;index:idx_llm_jobs_project_status_created,priority:1;index:idx_llm_jobs_project_public_id,priority:1;index"`
	UserID                uint                `gorm:"not null;index"`
	TokenRatio            uint                `gorm:"not null;default:0"`
	PublicID              string              `gorm:"type:string;size:128;index:idx_llm_jobs_project_public_id,priority:2;index"`
	APIType               LLMAPIType          `gorm:"not null"`
	Model                 string              `gorm:"type:string;size:255;not null"`
	BilledVram            uint64              `gorm:"not null;default:0"`
	Background            bool                `gorm:"not null;default:false"`
	Stream                bool                `gorm:"not null;default:false"`
	RequestBody           []byte              `gorm:"type:longblob"`
	TaskArgsJSON          string              `gorm:"type:longtext;not null"`
	BridgeClientTaskID    *uint               `gorm:"index"`
	Status                LLMJobStatus        `gorm:"not null;default:0;index:idx_llm_jobs_project_status_created,priority:2;index:idx_llm_jobs_terminal_cleanup,priority:1;index"`
	RawResultJSON         *string             `gorm:"type:longtext"`
	FormattedResultJSON   *string             `gorm:"type:longtext"`
	ErrorMessage          *string             `gorm:"type:text"`
	BillingStatus         LLMJobBillingStatus `gorm:"not null;default:0;index:idx_llm_jobs_terminal_cleanup,priority:2;index"`
	LLMCallRecordID       *uint               `gorm:"index"`
	TaskFeeGwei           *BigInt             `gorm:"type:string;size:255"`
	MedianPriorityGwei    *BigInt             `gorm:"type:string;size:255"`
	EstimatedNodeSeconds  *float64
	VramWeight            *float64
	ConstantSeconds       *float64
	SecondsPerInputToken  *float64
	SecondsPerOutputToken *float64
	StartedAt             *time.Time
	CompletedAt           *time.Time          `gorm:"index:idx_llm_jobs_terminal_cleanup,priority:3"`
}

func (LLMJob) TableName() string {
	return "llm_jobs"
}

func (j LLMJob) IsTerminal() bool {
	return j.Status == LLMJobStatusCompleted || j.Status == LLMJobStatusFailed
}
