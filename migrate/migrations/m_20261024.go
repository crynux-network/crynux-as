package migrations

import (
	"encoding/json"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskCallRecordForM20261024 struct {
	ID                   uint            `gorm:"primarykey"`
	CreatedAt            time.Time       `gorm:"not null"`
	ModelCacheHash       string          `gorm:"column:model_cache_hash;type:string;size:64;not null;default:''"`
	ExecutionDType       string          `gorm:"column:execution_dtype;type:string;size:64;not null;default:''"`
	QuantizationConfig   json.RawMessage `gorm:"type:longtext"`
	Components           json.RawMessage `gorm:"type:longtext"`
	ImageWidth           uint64          `gorm:"not null;default:0"`
	ImageHeight          uint64          `gorm:"not null;default:0"`
	ImageSteps           uint64          `gorm:"not null;default:0"`
	ImageCount           uint64          `gorm:"not null;default:0"`
}

func (taskCallRecordForM20261024) TableName() string {
	return "task_call_records"
}

type projectModelUsage10mForM20261024Old struct {
	ID        uint `gorm:"primarykey"`
	ProjectID uint `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_project_model_period"`
	Model     string `gorm:"type:string;size:255;not null;uniqueIndex:idx_project_model_usage_10m_project_model_period"`
	PeriodStart int64 `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_project_model_period"`
}

func (projectModelUsage10mForM20261024Old) TableName() string {
	return "project_model_usage_10m_stats"
}

type projectModelUsage10mForM20261024 struct {
	ID                   uint            `gorm:"primarykey"`
	CreatedAt            time.Time       `gorm:"not null"`
	UpdatedAt            time.Time       `gorm:"not null"`
	ProjectID            uint            `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:1"`
	UserID               uint            `gorm:"not null;index"`
	Model                string          `gorm:"type:string;size:255;not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:4"`
	ModelCacheHash       string          `gorm:"column:model_cache_hash;type:string;size:64;not null;default:'';uniqueIndex:idx_project_model_usage_10m_identity_period,priority:2"`
	ExecutionDType       string          `gorm:"column:execution_dtype;type:string;size:64;not null;default:'';uniqueIndex:idx_project_model_usage_10m_identity_period,priority:3"`
	TaskType             int8            `gorm:"not null;default:0"`
	QuantizationConfig   json.RawMessage `gorm:"type:longtext"`
	Components           json.RawMessage `gorm:"type:longtext"`
	PeriodStart          int64           `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:5;index"`
	RequestCount         uint64          `gorm:"not null;default:0"`
	SuccessCount         uint64          `gorm:"not null;default:0"`
	FailureCount         uint64          `gorm:"not null;default:0"`
	PromptTokens         uint64          `gorm:"not null;default:0"`
	CompletionTokens     uint64          `gorm:"not null;default:0"`
	TotalTokens          uint64          `gorm:"not null;default:0"`
	ImageCount           uint64          `gorm:"not null;default:0"`
	Credits              string          `gorm:"type:string;size:255;not null"`
}

func (projectModelUsage10mForM20261024) TableName() string {
	return "project_model_usage_10m_stats"
}

type projectModelUsageSnapshotForM20261024 struct {
	ID                   uint            `gorm:"primarykey"`
	CreatedAt            time.Time       `gorm:"not null"`
	UpdatedAt            time.Time       `gorm:"not null"`
	ProjectID            uint            `gorm:"not null"`
	RangeType            string          `gorm:"type:string;size:8;not null"`
	Rank                 uint            `gorm:"not null"`
	Model                string          `gorm:"type:string;size:255;not null"`
	ModelCacheHash       string          `gorm:"column:model_cache_hash;type:string;size:64;not null;default:''"`
	ExecutionDType       string          `gorm:"column:execution_dtype;type:string;size:64;not null;default:''"`
	TaskType             int8            `gorm:"not null;default:0"`
	QuantizationConfig   json.RawMessage `gorm:"type:longtext"`
	Components           json.RawMessage `gorm:"type:longtext"`
	RequestCount         uint64          `gorm:"not null;default:0"`
	SuccessCount         uint64          `gorm:"not null;default:0"`
	FailureCount         uint64          `gorm:"not null;default:0"`
	PromptTokens         uint64          `gorm:"not null;default:0"`
	CompletionTokens     uint64          `gorm:"not null;default:0"`
	TotalTokens          uint64          `gorm:"not null;default:0"`
	ImageCount           uint64          `gorm:"not null;default:0"`
	Credits              string          `gorm:"type:string;size:255;not null"`
	WindowStart          int64           `gorm:"not null"`
	WindowEnd            int64           `gorm:"not null"`
}

func (projectModelUsageSnapshotForM20261024) TableName() string {
	return "project_model_usage_snapshots"
}

type usageStatsProgressForM20261024 struct {
	ProjectID         uint  `gorm:"primarykey"`
	LastCallRecordID  uint64
}

func (usageStatsProgressForM20261024) TableName() string {
	return "usage_stats_progress"
}

func M20261024(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20261024",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				record := &taskCallRecordForM20261024{}
				for _, col := range []string{
					"ModelCacheHash",
					"ExecutionDType",
					"QuantizationConfig",
					"Components",
					"ImageWidth",
					"ImageHeight",
					"ImageSteps",
					"ImageCount",
				} {
					if err := m.AddColumn(record, col); err != nil {
						return err
					}
				}

				old10m := &projectModelUsage10mForM20261024Old{}
				if err := m.DropIndex(old10m, "idx_project_model_usage_10m_project_model_period"); err != nil {
					return err
				}
				tenMin := &projectModelUsage10mForM20261024{}
				for _, col := range []string{
					"ModelCacheHash",
					"ExecutionDType",
					"TaskType",
					"QuantizationConfig",
					"Components",
					"ImageCount",
				} {
					if err := m.AddColumn(tenMin, col); err != nil {
						return err
					}
				}
				if err := m.CreateIndex(tenMin, "idx_project_model_usage_10m_identity_period"); err != nil {
					return err
				}

				snap := &projectModelUsageSnapshotForM20261024{}
				for _, col := range []string{
					"ModelCacheHash",
					"ExecutionDType",
					"TaskType",
					"QuantizationConfig",
					"Components",
					"ImageCount",
				} {
					if err := m.AddColumn(snap, col); err != nil {
						return err
					}
				}

				if err := tx.Exec("DELETE FROM project_model_usage_10m_stats").Error; err != nil {
					return err
				}
				if err := tx.Exec("DELETE FROM project_model_usage_snapshots").Error; err != nil {
					return err
				}
				return tx.Model(&usageStatsProgressForM20261024{}).
					Where("project_id = ?", 0).
					Update("last_call_record_id", 0).Error
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				tenMin := &projectModelUsage10mForM20261024{}
				if err := m.DropIndex(tenMin, "idx_project_model_usage_10m_identity_period"); err != nil {
					return err
				}
				for _, col := range []string{
					"ImageCount",
					"Components",
					"QuantizationConfig",
					"TaskType",
					"ExecutionDType",
					"ModelCacheHash",
				} {
					if err := m.DropColumn(tenMin, col); err != nil {
						return err
					}
				}
				old10m := &projectModelUsage10mForM20261024Old{}
				if err := m.CreateIndex(old10m, "idx_project_model_usage_10m_project_model_period"); err != nil {
					return err
				}

				snap := &projectModelUsageSnapshotForM20261024{}
				for _, col := range []string{
					"ImageCount",
					"Components",
					"QuantizationConfig",
					"TaskType",
					"ExecutionDType",
					"ModelCacheHash",
				} {
					if err := m.DropColumn(snap, col); err != nil {
						return err
					}
				}

				record := &taskCallRecordForM20261024{}
				for _, col := range []string{
					"ImageCount",
					"ImageSteps",
					"ImageHeight",
					"ImageWidth",
					"Components",
					"QuantizationConfig",
					"ExecutionDType",
					"ModelCacheHash",
				} {
					if err := m.DropColumn(record, col); err != nil {
						return err
					}
				}
				return nil
			},
		},
	})
}
