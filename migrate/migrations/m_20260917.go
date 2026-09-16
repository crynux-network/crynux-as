package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmCallRecordCreditsPerGweiForM20260917 struct {
	CreditsPerGwei *string `gorm:"type:string;size:255"`
}

func (llmCallRecordCreditsPerGweiForM20260917) TableName() string {
	return "llm_call_records"
}

type llmCallRecordCreditsPerGweiRollbackForM20260917 struct {
	CreditsPerGwei *uint64
}

func (llmCallRecordCreditsPerGweiRollbackForM20260917) TableName() string {
	return "llm_call_records"
}

func M20260917(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260917",
			Migrate: func(tx *gorm.DB) error {
				callRecord := &llmCallRecordCreditsPerGweiForM20260917{}
				return tx.Migrator().AlterColumn(callRecord, "CreditsPerGwei")
			},
			Rollback: func(tx *gorm.DB) error {
				callRecord := &llmCallRecordCreditsPerGweiRollbackForM20260917{}
				return tx.Migrator().AlterColumn(callRecord, "CreditsPerGwei")
			},
		},
	})
}
