package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type referralRewardForM20261028 struct {
	PriorPurchaseCount uint64 `gorm:"not null;default:0"`
	TierMinPurchases   *uint64
	ExtraRatio         string `gorm:"type:string;size:64;not null;default:0"`
	ExtraAmount        string `gorm:"type:string;size:255;not null;default:0"`
}

func (referralRewardForM20261028) TableName() string {
	return "referral_rewards"
}

func M20261028(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261028",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Migrator().AddColumn(&referralRewardForM20261028{}, "PriorPurchaseCount"); err != nil {
				return err
			}
			if err := tx.Migrator().AddColumn(&referralRewardForM20261028{}, "TierMinPurchases"); err != nil {
				return err
			}
			if err := tx.Migrator().AddColumn(&referralRewardForM20261028{}, "ExtraRatio"); err != nil {
				return err
			}
			return tx.Migrator().AddColumn(&referralRewardForM20261028{}, "ExtraAmount")
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropColumn(&referralRewardForM20261028{}, "ExtraAmount"); err != nil {
				return err
			}
			if err := tx.Migrator().DropColumn(&referralRewardForM20261028{}, "ExtraRatio"); err != nil {
				return err
			}
			if err := tx.Migrator().DropColumn(&referralRewardForM20261028{}, "TierMinPurchases"); err != nil {
				return err
			}
			return tx.Migrator().DropColumn(&referralRewardForM20261028{}, "PriorPurchaseCount")
		},
	}})
}
