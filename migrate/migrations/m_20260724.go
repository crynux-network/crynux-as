package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type projectTokenRatioForM20260724 struct {
	TokenRatio uint `gorm:"not null;default:10"`
}

func (projectTokenRatioForM20260724) TableName() string {
	return "projects"
}

func M20260724(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260724",
			Migrate: func(tx *gorm.DB) error {
				return tx.Migrator().AddColumn(&projectTokenRatioForM20260724{}, "TokenRatio")
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropColumn(&projectTokenRatioForM20260724{}, "TokenRatio")
			},
		},
	})
}
