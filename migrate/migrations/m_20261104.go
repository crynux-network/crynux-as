package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskCallRecordForM20261104 struct {
	VideoWidth     uint64 `gorm:"not null;default:0"`
	VideoHeight    uint64 `gorm:"not null;default:0"`
	VideoNumFrames uint64 `gorm:"not null;default:0"`
	VideoSteps     uint64 `gorm:"not null;default:0"`
	VideoCount     uint64 `gorm:"not null;default:0"`
}

func (taskCallRecordForM20261104) TableName() string {
	return "task_call_records"
}

type accountUsageHourlyForM20261104 struct {
	VideoCount uint64 `gorm:"not null;default:0"`
}

func (accountUsageHourlyForM20261104) TableName() string {
	return "account_usage_hourly_stats"
}

type projectUsageHourlyForM20261104 struct {
	VideoCount uint64 `gorm:"not null;default:0"`
}

func (projectUsageHourlyForM20261104) TableName() string {
	return "project_usage_hourly_stats"
}

type projectModelUsage10mForM20261104Old struct {
	ProjectID      uint   `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:1"`
	ModelCacheHash string `gorm:"column:model_cache_hash;type:string;size:64;not null;default:'';uniqueIndex:idx_project_model_usage_10m_identity_period,priority:2"`
	ExecutionDType string `gorm:"column:execution_dtype;type:string;size:64;not null;default:'';uniqueIndex:idx_project_model_usage_10m_identity_period,priority:3"`
	Model          string `gorm:"type:string;size:255;not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:4"`
	PeriodStart    int64  `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:5"`
}

func (projectModelUsage10mForM20261104Old) TableName() string {
	return "project_model_usage_10m_stats"
}

type projectModelUsage10mForM20261104 struct {
	ProjectID      uint   `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:1"`
	ModelCacheHash string `gorm:"column:model_cache_hash;type:string;size:64;not null;default:'';uniqueIndex:idx_project_model_usage_10m_identity_period,priority:2"`
	DType          string `gorm:"column:dtype;type:string;size:64;not null;default:'';uniqueIndex:idx_project_model_usage_10m_identity_period,priority:3"`
	Model          string `gorm:"type:string;size:255;not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:4"`
	PeriodStart    int64  `gorm:"not null;uniqueIndex:idx_project_model_usage_10m_identity_period,priority:5"`
	VideoCount     uint64 `gorm:"not null;default:0"`
}

func (projectModelUsage10mForM20261104) TableName() string {
	return "project_model_usage_10m_stats"
}

type projectModelUsageSnapshotForM20261104 struct {
	VideoCount uint64 `gorm:"not null;default:0"`
}

func (projectModelUsageSnapshotForM20261104) TableName() string {
	return "project_model_usage_snapshots"
}

func M20261104(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261104",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()

			record := &taskCallRecordForM20261104{}
			if err := m.RenameColumn(record, "execution_dtype", "dtype"); err != nil {
				return err
			}
			for _, col := range []string{"VideoWidth", "VideoHeight", "VideoNumFrames", "VideoSteps", "VideoCount"} {
				if err := m.AddColumn(record, col); err != nil {
					return err
				}
			}

			if err := m.AddColumn(&accountUsageHourlyForM20261104{}, "VideoCount"); err != nil {
				return err
			}
			if err := m.AddColumn(&projectUsageHourlyForM20261104{}, "VideoCount"); err != nil {
				return err
			}

			if err := m.DropIndex(&projectModelUsage10mForM20261104Old{}, "idx_project_model_usage_10m_identity_period"); err != nil {
				return err
			}
			tenMin := &projectModelUsage10mForM20261104{}
			if err := m.RenameColumn(tenMin, "execution_dtype", "dtype"); err != nil {
				return err
			}
			if err := m.CreateIndex(tenMin, "idx_project_model_usage_10m_identity_period"); err != nil {
				return err
			}
			if err := m.AddColumn(tenMin, "VideoCount"); err != nil {
				return err
			}

			snap := &projectModelUsageSnapshotForM20261104{}
			if err := m.RenameColumn(snap, "execution_dtype", "dtype"); err != nil {
				return err
			}
			return m.AddColumn(snap, "VideoCount")
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()

			snap := &projectModelUsageSnapshotForM20261104{}
			if err := m.DropColumn(snap, "VideoCount"); err != nil {
				return err
			}
			if err := m.RenameColumn(snap, "dtype", "execution_dtype"); err != nil {
				return err
			}

			tenMin := &projectModelUsage10mForM20261104{}
			if err := m.DropColumn(tenMin, "VideoCount"); err != nil {
				return err
			}
			if err := m.DropIndex(tenMin, "idx_project_model_usage_10m_identity_period"); err != nil {
				return err
			}
			if err := m.RenameColumn(tenMin, "dtype", "execution_dtype"); err != nil {
				return err
			}
			if err := m.CreateIndex(&projectModelUsage10mForM20261104Old{}, "idx_project_model_usage_10m_identity_period"); err != nil {
				return err
			}

			if err := m.DropColumn(&projectUsageHourlyForM20261104{}, "VideoCount"); err != nil {
				return err
			}
			if err := m.DropColumn(&accountUsageHourlyForM20261104{}, "VideoCount"); err != nil {
				return err
			}

			record := &taskCallRecordForM20261104{}
			for _, col := range []string{"VideoCount", "VideoSteps", "VideoNumFrames", "VideoHeight", "VideoWidth"} {
				if err := m.DropColumn(record, col); err != nil {
					return err
				}
			}
			return m.RenameColumn(record, "dtype", "execution_dtype")
		},
	}})
}
