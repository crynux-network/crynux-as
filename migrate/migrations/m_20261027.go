package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type referralCodeForM20261027 struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
	UserID    uint      `gorm:"not null;index"`
	Code      string    `gorm:"type:string;size:32;not null;uniqueIndex"`
	Status    int8      `gorm:"not null;default:0;index"`
}

func (referralCodeForM20261027) TableName() string {
	return "referral_codes"
}

type referralBindingForM20261027 struct {
	ID             uint      `gorm:"primarykey"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
	InviteeUserID  uint      `gorm:"not null;uniqueIndex"`
	ReferrerUserID uint      `gorm:"not null;index"`
	ReferralCodeID uint      `gorm:"not null;index"`
}

func (referralBindingForM20261027) TableName() string {
	return "referral_bindings"
}

type referralRewardForM20261027 struct {
	ID                      uint      `gorm:"primarykey"`
	CreatedAt               time.Time `gorm:"not null"`
	UpdatedAt               time.Time `gorm:"not null"`
	ReferrerUserID          uint      `gorm:"not null;index:idx_referral_rewards_referrer_id,priority:1"`
	InviteeUserID           *uint     `gorm:"index"`
	Kind                    int8      `gorm:"not null;index:idx_referral_rewards_referrer_id,priority:2"`
	Amount                  string    `gorm:"type:string;size:255;not null"`
	DepositID               *uint     `gorm:"uniqueIndex"`
	MilestoneReferrerUserID *uint     `gorm:"uniqueIndex"`
	DepositCredits          string    `gorm:"type:string;size:255;not null;default:0"`
}

func (referralRewardForM20261027) TableName() string {
	return "referral_rewards"
}

func M20261027(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261027",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Migrator().CreateTable(&referralCodeForM20261027{}); err != nil {
				return err
			}
			if err := tx.Migrator().CreateTable(&referralBindingForM20261027{}); err != nil {
				return err
			}
			return tx.Migrator().CreateTable(&referralRewardForM20261027{})
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable(&referralRewardForM20261027{}); err != nil {
				return err
			}
			if err := tx.Migrator().DropTable(&referralBindingForM20261027{}); err != nil {
				return err
			}
			return tx.Migrator().DropTable(&referralCodeForM20261027{})
		},
	}})
}
