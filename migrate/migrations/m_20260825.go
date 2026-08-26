package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmCallRecordTaskFeeForM20260825 struct {
	TaskFeeGwei          *string  `gorm:"type:string;size:255"`
	MedianPriorityGwei   *string  `gorm:"type:string;size:255"`
	EstimatedNodeSeconds *float64
	VramWeight           *float64
}

func (llmCallRecordTaskFeeForM20260825) TableName() string {
	return "llm_call_records"
}

func M20260825(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260825",
			Migrate: func(tx *gorm.DB) error {
				if err := tx.Migrator().AddColumn(&llmCallRecordTaskFeeForM20260825{}, "TaskFeeGwei"); err != nil {
					return err
				}
				if err := tx.Migrator().AddColumn(&llmCallRecordTaskFeeForM20260825{}, "MedianPriorityGwei"); err != nil {
					return err
				}
				if err := tx.Migrator().AddColumn(&llmCallRecordTaskFeeForM20260825{}, "EstimatedNodeSeconds"); err != nil {
					return err
				}
				return tx.Migrator().AddColumn(&llmCallRecordTaskFeeForM20260825{}, "VramWeight")
			},
			Rollback: func(tx *gorm.DB) error {
				if err := tx.Migrator().DropColumn(&llmCallRecordTaskFeeForM20260825{}, "VramWeight"); err != nil {
					return err
				}
				if err := tx.Migrator().DropColumn(&llmCallRecordTaskFeeForM20260825{}, "EstimatedNodeSeconds"); err != nil {
					return err
				}
				if err := tx.Migrator().DropColumn(&llmCallRecordTaskFeeForM20260825{}, "MedianPriorityGwei"); err != nil {
					return err
				}
				return tx.Migrator().DropColumn(&llmCallRecordTaskFeeForM20260825{}, "TaskFeeGwei")
			},
		},
	})
}
