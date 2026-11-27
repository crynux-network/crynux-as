package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskJobResultDownloadHashForM20261127 struct {
	ResultDownloadHash *string `gorm:"column:result_download_hash;type:string;size:64;uniqueIndex:idx_task_jobs_result_download_hash"`
}

func (taskJobResultDownloadHashForM20261127) TableName() string {
	return "task_jobs"
}

func M20261127(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261127",
		Migrate: func(tx *gorm.DB) error {
			return tx.Migrator().AddColumn(&taskJobResultDownloadHashForM20261127{}, "ResultDownloadHash")
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropColumn(&taskJobResultDownloadHashForM20261127{}, "ResultDownloadHash")
		},
	}})
}
