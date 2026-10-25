package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type accountUsageHourlyForM20261026 struct {
	ImageCount uint64 `gorm:"not null;default:0"`
}

func (accountUsageHourlyForM20261026) TableName() string {
	return "account_usage_hourly_stats"
}

type projectUsageHourlyForM20261026 struct {
	ImageCount uint64 `gorm:"not null;default:0"`
}

func (projectUsageHourlyForM20261026) TableName() string {
	return "project_usage_hourly_stats"
}

func M20261026(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261026",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Migrator().AddColumn(&accountUsageHourlyForM20261026{}, "ImageCount"); err != nil {
				return err
			}
			return tx.Migrator().AddColumn(&projectUsageHourlyForM20261026{}, "ImageCount")
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropColumn(&accountUsageHourlyForM20261026{}, "ImageCount"); err != nil {
				return err
			}
			return tx.Migrator().DropColumn(&projectUsageHourlyForM20261026{}, "ImageCount")
		},
	}})
}
