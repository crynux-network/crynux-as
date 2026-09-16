package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type projectPriorityForM20260916 struct {
	TokenRatio   uint   `gorm:"not null;default:10"`
	PriorityGwei string `gorm:"type:string;size:255;not null"`
}

func (projectPriorityForM20260916) TableName() string {
	return "projects"
}

type llmJobPriorityForM20260916 struct {
	TokenRatio   uint   `gorm:"not null;default:0"`
	PriorityGwei string `gorm:"type:string;size:255;not null"`
}

func (llmJobPriorityForM20260916) TableName() string {
	return "llm_jobs"
}

type llmCallRecordPriorityForM20260916 struct {
	TokenRatio            uint    `gorm:"not null;default:0"`
	PriorityGwei          string  `gorm:"type:string;size:255;not null"`
	ReferencePriorityGwei *string `gorm:"type:string;size:255"`
}

func (llmCallRecordPriorityForM20260916) TableName() string {
	return "llm_call_records"
}

func M20260916(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260916",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				project := &projectPriorityForM20260916{}
				if err := m.DropColumn(project, "TokenRatio"); err != nil {
					return err
				}
				if err := m.AddColumn(project, "PriorityGwei"); err != nil {
					return err
				}

				job := &llmJobPriorityForM20260916{}
				if err := m.DropColumn(job, "TokenRatio"); err != nil {
					return err
				}
				if err := m.AddColumn(job, "PriorityGwei"); err != nil {
					return err
				}

				callRecord := &llmCallRecordPriorityForM20260916{}
				if err := m.DropColumn(callRecord, "TokenRatio"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "ReferencePriorityGwei"); err != nil {
					return err
				}
				return m.AddColumn(callRecord, "PriorityGwei")
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				callRecord := &llmCallRecordPriorityForM20260916{}
				if err := m.DropColumn(callRecord, "PriorityGwei"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "ReferencePriorityGwei"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "TokenRatio"); err != nil {
					return err
				}

				job := &llmJobPriorityForM20260916{}
				if err := m.DropColumn(job, "PriorityGwei"); err != nil {
					return err
				}
				if err := m.AddColumn(job, "TokenRatio"); err != nil {
					return err
				}

				project := &projectPriorityForM20260916{}
				if err := m.DropColumn(project, "PriorityGwei"); err != nil {
					return err
				}
				return m.AddColumn(project, "TokenRatio")
			},
		},
	})
}
