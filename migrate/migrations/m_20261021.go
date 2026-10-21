package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskJobPhaseForM20261021 struct {
	ID                    uint       `gorm:"primarykey"`
	CreatedAt             time.Time  `gorm:"not null"`
	UpdatedAt             time.Time  `gorm:"not null"`
	Phase                 string     `gorm:"type:string;size:32;not null;default:queued"`
	EstimatedCompletionAt *time.Time `gorm:"column:estimated_completion_at"`
}

func (taskJobPhaseForM20261021) TableName() string {
	return "task_jobs"
}

func M20261021(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20261021",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				job := &taskJobPhaseForM20261021{}
				if err := m.AddColumn(job, "Phase"); err != nil {
					return err
				}
				return m.AddColumn(job, "EstimatedCompletionAt")
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				job := &taskJobPhaseForM20261021{}
				if err := m.DropColumn(job, "EstimatedCompletionAt"); err != nil {
					return err
				}
				return m.DropColumn(job, "Phase")
			},
		},
	})
}
