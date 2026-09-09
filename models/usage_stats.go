package models

import "time"

const UsageStatsBaseProjectID uint = 0

type UsageStatsRangeType string

const (
	UsageStatsRange1h UsageStatsRangeType = "1h"
	UsageStatsRange1d UsageStatsRangeType = "1d"
	UsageStatsRange7d UsageStatsRangeType = "7d"
)

// AccountUsageHourlyStat stores per-account usage for one Unix hour.
type AccountUsageHourlyStat struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	CreatedAt        time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"not null"`
	UserID           uint      `json:"user_id" gorm:"not null;uniqueIndex:idx_account_usage_hourly_user_period"`
	PeriodStart      int64     `json:"period_start" gorm:"not null;uniqueIndex:idx_account_usage_hourly_user_period;index"`
	RequestCount     uint64    `json:"request_count" gorm:"not null;default:0"`
	SuccessCount     uint64    `json:"success_count" gorm:"not null;default:0"`
	FailureCount     uint64    `json:"failure_count" gorm:"not null;default:0"`
	PromptTokens     uint64    `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens uint64    `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      uint64    `json:"total_tokens" gorm:"not null;default:0"`
	Credits          BigInt    `json:"credits" gorm:"type:string;size:255;not null"`
}

func (AccountUsageHourlyStat) TableName() string {
	return "account_usage_hourly_stats"
}

// ProjectUsageHourlyStat stores per-project usage for one Unix hour.
type ProjectUsageHourlyStat struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	CreatedAt        time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"not null"`
	ProjectID        uint      `json:"project_id" gorm:"not null;uniqueIndex:idx_project_usage_hourly_project_period"`
	UserID           uint      `json:"user_id" gorm:"not null;index"`
	PeriodStart      int64     `json:"period_start" gorm:"not null;uniqueIndex:idx_project_usage_hourly_project_period;index"`
	RequestCount     uint64    `json:"request_count" gorm:"not null;default:0"`
	SuccessCount     uint64    `json:"success_count" gorm:"not null;default:0"`
	FailureCount     uint64    `json:"failure_count" gorm:"not null;default:0"`
	PromptTokens     uint64    `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens uint64    `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      uint64    `json:"total_tokens" gorm:"not null;default:0"`
	Credits          BigInt    `json:"credits" gorm:"type:string;size:255;not null"`
}

func (ProjectUsageHourlyStat) TableName() string {
	return "project_usage_hourly_stats"
}

// ProjectModelUsage10mStat stores per-project per-model usage for one Unix 10-minute period.
type ProjectModelUsage10mStat struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	CreatedAt        time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"not null"`
	ProjectID        uint      `json:"project_id" gorm:"not null;uniqueIndex:idx_project_model_usage_10m_project_model_period"`
	UserID           uint      `json:"user_id" gorm:"not null;index"`
	Model            string    `json:"model" gorm:"type:string;size:255;not null;uniqueIndex:idx_project_model_usage_10m_project_model_period"`
	PeriodStart      int64     `json:"period_start" gorm:"not null;uniqueIndex:idx_project_model_usage_10m_project_model_period;index"`
	RequestCount     uint64    `json:"request_count" gorm:"not null;default:0"`
	SuccessCount     uint64    `json:"success_count" gorm:"not null;default:0"`
	FailureCount     uint64    `json:"failure_count" gorm:"not null;default:0"`
	PromptTokens     uint64    `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens uint64    `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      uint64    `json:"total_tokens" gorm:"not null;default:0"`
	Credits          BigInt    `json:"credits" gorm:"type:string;size:255;not null"`
}

func (ProjectModelUsage10mStat) TableName() string {
	return "project_model_usage_10m_stats"
}

// ProjectDurationUsage10mStat stores completion-duration bucket counts for one Unix 10-minute period.
type ProjectDurationUsage10mStat struct {
	ID             uint      `json:"id" gorm:"primarykey"`
	CreatedAt      time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"not null"`
	ProjectID      uint      `json:"project_id" gorm:"not null;uniqueIndex:idx_project_duration_usage_10m_project_period_bucket"`
	UserID         uint      `json:"user_id" gorm:"not null;index"`
	PeriodStart    int64     `json:"period_start" gorm:"not null;uniqueIndex:idx_project_duration_usage_10m_project_period_bucket;index"`
	DurationBucket string    `json:"duration_bucket" gorm:"type:string;size:64;not null;uniqueIndex:idx_project_duration_usage_10m_project_period_bucket"`
	RequestCount   uint64    `json:"request_count" gorm:"not null;default:0"`
}

func (ProjectDurationUsage10mStat) TableName() string {
	return "project_duration_usage_10m_stats"
}

// ProjectModelUsageSnapshot stores Top-10 model rows for a project and range.
type ProjectModelUsageSnapshot struct {
	ID               uint                `json:"id" gorm:"primarykey"`
	CreatedAt        time.Time           `json:"created_at" gorm:"not null"`
	UpdatedAt        time.Time           `json:"updated_at" gorm:"not null"`
	ProjectID        uint                `json:"project_id" gorm:"not null;uniqueIndex:idx_project_model_usage_snapshots_project_range_rank"`
	RangeType        UsageStatsRangeType `json:"range_type" gorm:"type:string;size:8;not null;uniqueIndex:idx_project_model_usage_snapshots_project_range_rank"`
	Rank             uint                `json:"rank" gorm:"not null;uniqueIndex:idx_project_model_usage_snapshots_project_range_rank"`
	Model            string              `json:"model" gorm:"type:string;size:255;not null"`
	RequestCount     uint64              `json:"request_count" gorm:"not null;default:0"`
	SuccessCount     uint64              `json:"success_count" gorm:"not null;default:0"`
	FailureCount     uint64              `json:"failure_count" gorm:"not null;default:0"`
	PromptTokens     uint64              `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens uint64              `json:"completion_tokens" gorm:"not null;default:0"`
	TotalTokens      uint64              `json:"total_tokens" gorm:"not null;default:0"`
	Credits          BigInt              `json:"credits" gorm:"type:string;size:255;not null"`
	WindowStart      int64               `json:"window_start" gorm:"not null"`
	WindowEnd        int64               `json:"window_end" gorm:"not null"`
}

func (ProjectModelUsageSnapshot) TableName() string {
	return "project_model_usage_snapshots"
}

// ProjectDurationHistogramSnapshot stores displayable completion-duration buckets.
type ProjectDurationHistogramSnapshot struct {
	ID           uint                `json:"id" gorm:"primarykey"`
	CreatedAt    time.Time           `json:"created_at" gorm:"not null"`
	UpdatedAt    time.Time           `json:"updated_at" gorm:"not null"`
	ProjectID    uint                `json:"project_id" gorm:"not null;uniqueIndex:idx_project_duration_histogram_snapshots_project_range_bucket"`
	RangeType    UsageStatsRangeType `json:"range_type" gorm:"type:string;size:8;not null;uniqueIndex:idx_project_duration_histogram_snapshots_project_range_bucket"`
	BucketIndex  uint                `json:"bucket_index" gorm:"not null;uniqueIndex:idx_project_duration_histogram_snapshots_project_range_bucket"`
	BucketLabel  string              `json:"bucket_label" gorm:"type:string;size:128;not null"`
	MinDurationMs uint64             `json:"min_duration_ms" gorm:"not null;default:0"`
	MaxDurationMs *uint64            `json:"max_duration_ms"`
	RequestCount uint64              `json:"request_count" gorm:"not null;default:0"`
	WindowStart  int64               `json:"window_start" gorm:"not null"`
	WindowEnd    int64               `json:"window_end" gorm:"not null"`
}

func (ProjectDurationHistogramSnapshot) TableName() string {
	return "project_duration_histogram_snapshots"
}

// UsageStatsProgress tracks base aggregation cursor and per-project snapshot refresh state.
// ProjectID == 0 is the singleton base-worker cursor row.
type UsageStatsProgress struct {
	ID                uint      `json:"id" gorm:"primarykey"`
	CreatedAt         time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt         time.Time `json:"updated_at" gorm:"not null"`
	ProjectID         uint      `json:"project_id" gorm:"not null;uniqueIndex"`
	LastCallRecordID  uint      `json:"last_call_record_id" gorm:"not null;default:0"`
	DirtyGeneration   uint64    `json:"dirty_generation" gorm:"not null;default:0"`
	ClaimedGeneration uint64    `json:"claimed_generation" gorm:"not null;default:0"`
	ClaimedAt         *time.Time `json:"claimed_at"`
}

func (UsageStatsProgress) TableName() string {
	return "usage_stats_progress"
}
