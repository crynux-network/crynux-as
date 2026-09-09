package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmCallRecordForM20260908 struct {
	UserID               uint      `gorm:"not null;index;default:0"`
	LLMJobID             *uint     `gorm:"uniqueIndex"`
	TokenUsageApplicable int8      `gorm:"not null;default:1"`
	AcceptedAt           time.Time `gorm:"not null;index;default:1970-01-01 00:00:00"`
	CompletedAt          time.Time `gorm:"not null;index;default:1970-01-01 00:00:00"`
}

func (llmCallRecordForM20260908) TableName() string {
	return "llm_call_records"
}

type llmJobForM20260908 struct {
	UserID     uint `gorm:"not null;index;default:0"`
	TokenRatio uint `gorm:"not null;default:0"`
}

func (llmJobForM20260908) TableName() string {
	return "llm_jobs"
}

type accountUsageHourlyStatForM20260908 struct {
	ID               uint      `gorm:"primarykey"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
	UserID           uint      `gorm:"not null;uniqueIndex:idx_account_usage_hourly_user_period"`
	PeriodStart      int64     `gorm:"not null;uniqueIndex:idx_account_usage_hourly_user_period;index"`
	RequestCount     uint64    `gorm:"not null;default:0"`
	SuccessCount     uint64    `gorm:"not null;default:0"`
	FailureCount     uint64    `gorm:"not null;default:0"`
	PromptTokens     uint64    `gorm:"not null;default:0"`
	CompletionTokens uint64    `gorm:"not null;default:0"`
	TotalTokens      uint64    `gorm:"not null;default:0"`
	Credits          string    `gorm:"type:string;size:255;not null"`
}

func (accountUsageHourlyStatForM20260908) TableName() string {
	return "account_usage_hourly_stats"
}

type projectUsageHourlyStatForM20260908 struct {
	ID               uint      `gorm:"primarykey"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
	ProjectID        uint      `gorm:"not null;uniqueIndex:idx_project_usage_hourly_project_period"`
	UserID           uint      `gorm:"not null;index"`
	PeriodStart      int64     `gorm:"not null;uniqueIndex:idx_project_usage_hourly_project_period;index"`
	RequestCount     uint64    `gorm:"not null;default:0"`
	SuccessCount     uint64    `gorm:"not null;default:0"`
	FailureCount     uint64    `gorm:"not null;default:0"`
	PromptTokens     uint64    `gorm:"not null;default:0"`
	CompletionTokens uint64    `gorm:"not null;default:0"`
	TotalTokens      uint64    `gorm:"not null;default:0"`
	Credits          string    `gorm:"type:string;size:255;not null"`
}

func (projectUsageHourlyStatForM20260908) TableName() string {
	return "project_usage_hourly_stats"
}

type projectModelUsage10mStatForM20260908 struct {
	ID               uint      `gorm:"primarykey"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
	ProjectID        uint      `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_project_model_period"`
	UserID           uint      `gorm:"not null;index"`
	Model            string    `gorm:"type:string;size:255;not null;uniqueIndex:idx_project_model_usage_10m_project_model_period"`
	PeriodStart      int64     `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_project_model_period;index"`
	RequestCount     uint64    `gorm:"not null;default:0"`
	SuccessCount     uint64    `gorm:"not null;default:0"`
	FailureCount     uint64    `gorm:"not null;default:0"`
	PromptTokens     uint64    `gorm:"not null;default:0"`
	CompletionTokens uint64    `gorm:"not null;default:0"`
	TotalTokens      uint64    `gorm:"not null;default:0"`
	Credits          string    `gorm:"type:string;size:255;not null"`
}

func (projectModelUsage10mStatForM20260908) TableName() string {
	return "project_model_usage_10m_stats"
}

type projectDurationUsage10mStatForM20260908 struct {
	ID             uint      `gorm:"primarykey"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
	ProjectID      uint      `gorm:"not null;uniqueIndex:idx_project_duration_usage_10m_project_period_bucket"`
	UserID         uint      `gorm:"not null;index"`
	PeriodStart    int64     `gorm:"not null;uniqueIndex:idx_project_duration_usage_10m_project_period_bucket;index"`
	DurationBucket string    `gorm:"type:string;size:64;not null;uniqueIndex:idx_project_duration_usage_10m_project_period_bucket"`
	RequestCount   uint64    `gorm:"not null;default:0"`
}

func (projectDurationUsage10mStatForM20260908) TableName() string {
	return "project_duration_usage_10m_stats"
}

type projectModelUsageSnapshotForM20260908 struct {
	ID               uint      `gorm:"primarykey"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
	ProjectID        uint      `gorm:"not null;uniqueIndex:idx_project_model_usage_snapshots_project_range_rank"`
	RangeType        string    `gorm:"type:string;size:8;not null;uniqueIndex:idx_project_model_usage_snapshots_project_range_rank"`
	Rank             uint      `gorm:"not null;uniqueIndex:idx_project_model_usage_snapshots_project_range_rank"`
	Model            string    `gorm:"type:string;size:255;not null"`
	RequestCount     uint64    `gorm:"not null;default:0"`
	SuccessCount     uint64    `gorm:"not null;default:0"`
	FailureCount     uint64    `gorm:"not null;default:0"`
	PromptTokens     uint64    `gorm:"not null;default:0"`
	CompletionTokens uint64    `gorm:"not null;default:0"`
	TotalTokens      uint64    `gorm:"not null;default:0"`
	Credits          string    `gorm:"type:string;size:255;not null"`
	WindowStart      int64     `gorm:"not null"`
	WindowEnd        int64     `gorm:"not null"`
}

func (projectModelUsageSnapshotForM20260908) TableName() string {
	return "project_model_usage_snapshots"
}

type projectDurationHistogramSnapshotForM20260908 struct {
	ID            uint      `gorm:"primarykey"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
	ProjectID     uint      `gorm:"not null;uniqueIndex:idx_project_duration_histogram_snapshots_project_range_bucket"`
	RangeType     string    `gorm:"type:string;size:8;not null;uniqueIndex:idx_project_duration_histogram_snapshots_project_range_bucket"`
	BucketIndex   uint      `gorm:"not null;uniqueIndex:idx_project_duration_histogram_snapshots_project_range_bucket"`
	BucketLabel   string    `gorm:"type:string;size:128;not null"`
	MinDurationMs uint64    `gorm:"not null;default:0"`
	MaxDurationMs *uint64
	RequestCount  uint64 `gorm:"not null;default:0"`
	WindowStart   int64  `gorm:"not null"`
	WindowEnd     int64  `gorm:"not null"`
}

func (projectDurationHistogramSnapshotForM20260908) TableName() string {
	return "project_duration_histogram_snapshots"
}

type usageStatsProgressForM20260908 struct {
	ID                uint      `gorm:"primarykey"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
	ProjectID         uint      `gorm:"not null;uniqueIndex"`
	LastCallRecordID  uint      `gorm:"not null;default:0"`
	DirtyGeneration   uint64    `gorm:"not null;default:0"`
	ClaimedGeneration uint64    `gorm:"not null;default:0"`
	ClaimedAt         *time.Time
}

func (usageStatsProgressForM20260908) TableName() string {
	return "usage_stats_progress"
}

type projectUsageStatForM20260908Drop struct {
	ID uint `gorm:"primarykey"`
}

func (projectUsageStatForM20260908Drop) TableName() string {
	return "project_usage_stats"
}

func M20260908(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260908",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				callRecord := &llmCallRecordForM20260908{}
				if err := m.AddColumn(callRecord, "UserID"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "LLMJobID"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "TokenUsageApplicable"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "AcceptedAt"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "CompletedAt"); err != nil {
					return err
				}

				job := &llmJobForM20260908{}
				if err := m.AddColumn(job, "UserID"); err != nil {
					return err
				}
				if err := m.AddColumn(job, "TokenRatio"); err != nil {
					return err
				}

				if err := m.CreateTable(&accountUsageHourlyStatForM20260908{}); err != nil {
					return err
				}
				if err := m.CreateTable(&projectUsageHourlyStatForM20260908{}); err != nil {
					return err
				}
				if err := m.CreateTable(&projectModelUsage10mStatForM20260908{}); err != nil {
					return err
				}
				if err := m.CreateTable(&projectDurationUsage10mStatForM20260908{}); err != nil {
					return err
				}
				if err := m.CreateTable(&projectModelUsageSnapshotForM20260908{}); err != nil {
					return err
				}
				if err := m.CreateTable(&projectDurationHistogramSnapshotForM20260908{}); err != nil {
					return err
				}
				if err := m.CreateTable(&usageStatsProgressForM20260908{}); err != nil {
					return err
				}
				return m.DropTable(&projectUsageStatForM20260908Drop{})
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				type projectUsageStatRestore struct {
					ID               uint      `gorm:"primarykey"`
					CreatedAt        time.Time `gorm:"not null"`
					UpdatedAt        time.Time `gorm:"not null"`
					ProjectID        uint      `gorm:"not null;uniqueIndex:idx_project_usage_stats_project_period"`
					PeriodStart      time.Time `gorm:"not null;uniqueIndex:idx_project_usage_stats_project_period;index"`
					CallCount        uint64    `gorm:"not null;default:0"`
					SuccessCount     uint64    `gorm:"not null;default:0"`
					FailureCount     uint64    `gorm:"not null;default:0"`
					PromptTokens     uint64    `gorm:"not null;default:0"`
					CompletionTokens uint64    `gorm:"not null;default:0"`
					TotalTokens      uint64    `gorm:"not null;default:0"`
					Credits          string    `gorm:"type:string;size:255;not null"`
				}
				if err := m.CreateTable(&projectUsageStatRestore{}); err != nil {
					return err
				}
				if err := m.DropTable(&usageStatsProgressForM20260908{}); err != nil {
					return err
				}
				if err := m.DropTable(&projectDurationHistogramSnapshotForM20260908{}); err != nil {
					return err
				}
				if err := m.DropTable(&projectModelUsageSnapshotForM20260908{}); err != nil {
					return err
				}
				if err := m.DropTable(&projectDurationUsage10mStatForM20260908{}); err != nil {
					return err
				}
				if err := m.DropTable(&projectModelUsage10mStatForM20260908{}); err != nil {
					return err
				}
				if err := m.DropTable(&projectUsageHourlyStatForM20260908{}); err != nil {
					return err
				}
				if err := m.DropTable(&accountUsageHourlyStatForM20260908{}); err != nil {
					return err
				}

				job := &llmJobForM20260908{}
				if err := m.DropColumn(job, "TokenRatio"); err != nil {
					return err
				}
				if err := m.DropColumn(job, "UserID"); err != nil {
					return err
				}

				callRecord := &llmCallRecordForM20260908{}
				if err := m.DropColumn(callRecord, "CompletedAt"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "AcceptedAt"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "TokenUsageApplicable"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "LLMJobID"); err != nil {
					return err
				}
				return m.DropColumn(callRecord, "UserID")
			},
		},
	})
}
