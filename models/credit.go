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

// CreditEvent is the append-only Credits ledger. RefID points to the source
// record of the event: the deposit ID for deposit events, and the LLM call
// record ID for LLM charge events. The (Type, RefID) pair is unique so one
// source record produces at most one ledger event.
type CreditEvent struct {
	ID        uint              `json:"id" gorm:"primarykey"`
	CreatedAt time.Time         `json:"created_at" gorm:"not null"`
	UserID    uint              `json:"user_id" gorm:"not null;index"`
	Amount    BigInt            `json:"amount" gorm:"type:string;size:255;not null"`
	Type      CreditEventType   `json:"type" gorm:"not null;uniqueIndex:idx_credit_events_type_ref"`
	RefID     uint              `json:"ref_id" gorm:"not null;uniqueIndex:idx_credit_events_type_ref"`
	Status    CreditEventStatus `json:"status" gorm:"not null;default:0;index"`
}
