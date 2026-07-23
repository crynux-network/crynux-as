package models

import "time"

type CreditAccount struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"not null"`
	UserID    uint      `json:"user_id" gorm:"not null;uniqueIndex"`
	Balance   BigInt    `json:"balance" gorm:"type:string;size:255;not null"`
}

type CreditEventType int8

const (
	CreditEventTypeDeposit   CreditEventType = 0
	CreditEventTypeLLMCharge CreditEventType = 1
)

type CreditEventStatus int8

const (
	CreditEventStatusPending CreditEventStatus = iota
	CreditEventStatusProcessed
	CreditEventStatusInvalid
)

type CreditEvent struct {
	ID        uint              `json:"id" gorm:"primarykey"`
	CreatedAt time.Time         `json:"created_at" gorm:"not null"`
	UserID    uint              `json:"user_id" gorm:"not null;index"`
	Amount    BigInt            `json:"amount" gorm:"type:string;size:255;not null"`
	Type      CreditEventType   `json:"type" gorm:"not null;index"`
	Status    CreditEventStatus `json:"status" gorm:"not null;default:0;index"`
	Reason    string            `json:"reason" gorm:"type:string;size:255;not null;uniqueIndex"`
}
