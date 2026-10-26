package models

import "time"

type ReferralCodeStatus int8

const (
	ReferralCodeStatusActive  ReferralCodeStatus = 0
	ReferralCodeStatusRevoked ReferralCodeStatus = 1
)

type ReferralCode struct {
	ID        uint               `json:"id" gorm:"primarykey"`
	CreatedAt time.Time          `json:"created_at" gorm:"not null"`
	UpdatedAt time.Time          `json:"updated_at" gorm:"not null"`
	UserID    uint               `json:"user_id" gorm:"not null;index"`
	Code      string             `json:"code" gorm:"type:string;size:32;not null;uniqueIndex"`
	Status    ReferralCodeStatus `json:"status" gorm:"not null;default:0;index"`
}

type ReferralBinding struct {
	ID             uint      `json:"id" gorm:"primarykey"`
	CreatedAt      time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt      time.Time `json:"updated_at" gorm:"not null"`
	InviteeUserID  uint      `json:"invitee_user_id" gorm:"not null;uniqueIndex"`
	ReferrerUserID uint      `json:"referrer_user_id" gorm:"not null;index"`
	ReferralCodeID uint      `json:"referral_code_id" gorm:"not null;index"`
}

type ReferralRewardKind int8

const (
	ReferralRewardKindDepositReward  ReferralRewardKind = 0
	ReferralRewardKindMilestoneBonus ReferralRewardKind = 1
)

type ReferralReward struct {
	ID                      uint               `json:"id" gorm:"primarykey"`
	CreatedAt               time.Time          `json:"created_at" gorm:"not null"`
	UpdatedAt               time.Time          `json:"updated_at" gorm:"not null"`
	ReferrerUserID          uint               `json:"referrer_user_id" gorm:"not null;index:idx_referral_rewards_referrer_id,priority:1"`
	InviteeUserID           *uint              `json:"invitee_user_id" gorm:"index"`
	Kind                    ReferralRewardKind `json:"kind" gorm:"not null;index:idx_referral_rewards_referrer_id,priority:2"`
	Amount                  BigInt             `json:"amount" gorm:"type:string;size:255;not null"`
	DepositID               *uint              `json:"deposit_id" gorm:"uniqueIndex"`
	MilestoneReferrerUserID *uint              `json:"milestone_referrer_user_id" gorm:"uniqueIndex"`
	DepositCredits          BigInt             `json:"deposit_credits" gorm:"type:string;size:255;not null;default:0"`
	PriorPurchaseCount      uint64             `json:"prior_purchase_count" gorm:"not null;default:0"`
	TierMinPurchases        *uint64            `json:"tier_min_purchases"`
	ExtraRatio              string             `json:"extra_ratio" gorm:"type:string;size:64;not null;default:0"`
	ExtraAmount             BigInt             `json:"extra_amount" gorm:"type:string;size:255;not null;default:0"`
}
