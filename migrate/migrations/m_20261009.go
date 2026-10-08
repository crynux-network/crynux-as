package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskJobDropRepeatNumForM20261009 struct {
	RepeatNum uint64 `gorm:"column:repeat_num;not null;default:1"`
}

func (taskJobDropRepeatNumForM20261009) TableName() string {
	return "task_jobs"
}

type taskJobAddRepeatNumForM20261009 struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
	RepeatNum uint64    `gorm:"column:repeat_num;not null;default:1"`
}

func (taskJobAddRepeatNumForM20261009) TableName() string {
	return "task_jobs"
}

func M20261009(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20261009",
			Migrate: func(tx *gorm.DB) error {
				return tx.Migrator().DropColumn(&taskJobDropRepeatNumForM20261009{}, "RepeatNum")
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().AddColumn(&taskJobAddRepeatNumForM20261009{}, "RepeatNum")
			},
		},
	})
}
