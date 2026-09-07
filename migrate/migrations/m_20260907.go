package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmJobForM20260907 struct {
	ConstantSeconds       *float64
	SecondsPerInputToken  *float64
	SecondsPerOutputToken *float64
}

func (llmJobForM20260907) TableName() string {
	return "llm_jobs"
}

func M20260907(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260907",
			Migrate: func(tx *gorm.DB) error {
				job := &llmJobForM20260907{}
				m := tx.Migrator()
				if err := m.AddColumn(job, "ConstantSeconds"); err != nil {
					return err
				}
				if err := m.AddColumn(job, "SecondsPerInputToken"); err != nil {
					return err
				}
				return m.AddColumn(job, "SecondsPerOutputToken")
			},
			Rollback: func(tx *gorm.DB) error {
				job := &llmJobForM20260907{}
				m := tx.Migrator()
				if err := m.DropColumn(job, "ConstantSeconds"); err != nil {
					return err
				}
				if err := m.DropColumn(job, "SecondsPerInputToken"); err != nil {
					return err
				}
				return m.DropColumn(job, "SecondsPerOutputToken")
			},
		},
	})
}
