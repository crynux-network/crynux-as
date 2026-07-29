package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmCallRecordTokenRatioForM20260729 struct {
	TokenRatio uint `gorm:"not null;default:0"`
}

func (llmCallRecordTokenRatioForM20260729) TableName() string {
	return "llm_call_records"
}

func M20260729(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260729",
			Migrate: func(tx *gorm.DB) error {
				return tx.Migrator().AddColumn(&llmCallRecordTokenRatioForM20260729{}, "TokenRatio")
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropColumn(&llmCallRecordTokenRatioForM20260729{}, "TokenRatio")
			},
		},
	})
}
