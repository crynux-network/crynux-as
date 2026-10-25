package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskJobQueueTimeoutForM20261025 struct {
	QueueTimeout uint64 `gorm:"column:queue_timeout;not null;default:0"`
}

func (taskJobQueueTimeoutForM20261025) TableName() string { return "task_jobs" }

func M20261025(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261025",
		Migrate: func(tx *gorm.DB) error {
			return tx.Migrator().AddColumn(&taskJobQueueTimeoutForM20261025{}, "QueueTimeout")
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropColumn(&taskJobQueueTimeoutForM20261025{}, "QueueTimeout")
		},
	}})
}
