package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type projectForM20261030 struct {
	AutoMaxCreditsPerTask string  `gorm:"type:string;size:255;not null;default:100"`
	AutoMaxPriorityGwei   *string `gorm:"type:string;size:255"`
}

func (projectForM20261030) TableName() string {
	return "projects"
}

func M20261030(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261030",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Migrator().AddColumn(&projectForM20261030{}, "AutoMaxCreditsPerTask"); err != nil {
				return err
			}
			if err := tx.Exec("UPDATE projects SET auto_max_credits_per_task = ?", "100").Error; err != nil {
				return err
			}
			return tx.Migrator().DropColumn(&projectForM20261030{}, "AutoMaxPriorityGwei")
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().AddColumn(&projectForM20261030{}, "AutoMaxPriorityGwei"); err != nil {
				return err
			}
			return tx.Migrator().DropColumn(&projectForM20261030{}, "AutoMaxCreditsPerTask")
		},
	}})
}
