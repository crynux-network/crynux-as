package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type sharedAPIKeyForM20261031 struct {
	UsedInputTokens  uint64  `gorm:"not null;default:0"`
	UsedOutputTokens uint64  `gorm:"not null;default:0"`
	MaxInputTokens   *uint64
	MaxOutputTokens  *uint64
}

func (sharedAPIKeyForM20261031) TableName() string {
	return "shared_api_keys"
}

func M20261031(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261031",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Migrator().AddColumn(&sharedAPIKeyForM20261031{}, "UsedInputTokens"); err != nil {
				return err
			}
			if err := tx.Migrator().AddColumn(&sharedAPIKeyForM20261031{}, "UsedOutputTokens"); err != nil {
				return err
			}
			if err := tx.Migrator().AddColumn(&sharedAPIKeyForM20261031{}, "MaxInputTokens"); err != nil {
				return err
			}
			return tx.Migrator().AddColumn(&sharedAPIKeyForM20261031{}, "MaxOutputTokens")
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropColumn(&sharedAPIKeyForM20261031{}, "MaxOutputTokens"); err != nil {
				return err
			}
			if err := tx.Migrator().DropColumn(&sharedAPIKeyForM20261031{}, "MaxInputTokens"); err != nil {
				return err
			}
			if err := tx.Migrator().DropColumn(&sharedAPIKeyForM20261031{}, "UsedOutputTokens"); err != nil {
				return err
			}
			return tx.Migrator().DropColumn(&sharedAPIKeyForM20261031{}, "UsedInputTokens")
		},
	}})
}
