package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmCallRecordBilledVramForM20260725 struct {
	BilledVram uint64 `gorm:"not null;default:0"`
}

func (llmCallRecordBilledVramForM20260725) TableName() string {
	return "llm_call_records"
}

func M20260725(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260725",
			Migrate: func(tx *gorm.DB) error {
				return tx.Migrator().AddColumn(&llmCallRecordBilledVramForM20260725{}, "BilledVram")
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropColumn(&llmCallRecordBilledVramForM20260725{}, "BilledVram")
			},
		},
	})
}
