package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type usageStatsPendingCallRecordForM20261102 struct {
	CallRecordID uint `gorm:"primaryKey;autoIncrement:false"`
}

func (usageStatsPendingCallRecordForM20261102) TableName() string {
	return "usage_stats_pending_call_records"
}

func M20261102(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261102",
		Migrate: func(tx *gorm.DB) error {
			return tx.Migrator().CreateTable(&usageStatsPendingCallRecordForM20261102{})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Migrator().DropTable(&usageStatsPendingCallRecordForM20261102{})
		},
	}})
}
