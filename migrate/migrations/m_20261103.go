package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskJobForM20261103 struct {
	TaskVersion *string `gorm:"column:task_version;type:string;size:64"`
}

func (taskJobForM20261103) TableName() string {
	return "task_jobs"
}

func M20261103(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261103",
		Migrate: func(tx *gorm.DB) error {
			return tx.Migrator().DropColumn(&taskJobForM20261103{}, "TaskVersion")
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().AddColumn(&taskJobForM20261103{}, "TaskVersion")
		},
	}})
}
