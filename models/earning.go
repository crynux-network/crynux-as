package models

import "time"

type EarningEventType int8

const (
	EarningEventTypeTaskIncome          EarningEventType = 0
	EarningEventTypeWithdrawalFeeIncome EarningEventType = 2
	EarningEventTypeWithdraw            EarningEventType = 6
	EarningEventTypeWithdrawRefund      EarningEventType = 7
)

type EarningEventStatus int8

const (
	EarningEventStatusProcessed EarningEventStatus = 1
	EarningEventStatusInvalid   EarningEventStatus = 2
)

type EarningSourceType string

const (
	EarningSourceTypeTaskCallRecord EarningSourceType = "task_call_record"
	EarningSourceTypeWithdrawal     EarningSourceType = "withdrawal"
)

type EarningWithdrawalStatus int8

const (
	EarningWithdrawalStatusPending EarningWithdrawalStatus = iota
	EarningWithdrawalStatusSuccess
	EarningWithdrawalStatusFailed
)

// EarningAccount holds one Key Owner CNX earning balance.
type EarningAccount struct {
	ID               uint      `json:"id" gorm:"primarykey"`
	CreatedAt        time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"not null"`
	UserID           uint      `json:"user_id" gorm:"not null;uniqueIndex"`
	Available        BigInt    `json:"available" gorm:"type:string;size:255;not null;default:0"`
	WithdrawalLocked BigInt    `json:"withdrawal_locked" gorm:"type:string;size:255;not null;default:0"`
}

func (EarningAccount) TableName() string {
	return "earning_accounts"
}

// EarningEvent is the permanent Key Owner CNX ledger entry and Wallet event source.
type EarningEvent struct {
	ID              uint               `json:"id" gorm:"primarykey;index:idx_earning_events_status_id,priority:2"`
	CreatedAt       time.Time          `json:"created_at" gorm:"not null"`
	UpdatedAt       time.Time          `json:"updated_at" gorm:"not null"`
	EarningAccountID *uint             `json:"earning_account_id" gorm:"index"`
	Address         string             `json:"address" gorm:"type:string;size:42;not null"`
	EventType       EarningEventType   `json:"event_type" gorm:"not null;uniqueIndex:idx_earning_events_type_source,priority:1"`
	Amount          BigInt             `json:"amount" gorm:"type:string;size:255;not null"`
	SourceType      EarningSourceType  `json:"source_type" gorm:"type:string;size:64;not null;uniqueIndex:idx_earning_events_type_source,priority:2"`
	SourceID        uint               `json:"source_id" gorm:"not null;uniqueIndex:idx_earning_events_type_source,priority:3"`
	Status          EarningEventStatus `json:"status" gorm:"not null;default:1;index:idx_earning_events_status_id,priority:1"`
	ProcessedAt     *time.Time         `json:"processed_at"`
	Payload         string             `json:"payload" gorm:"type:longtext;not null"`
}

func (EarningEvent) TableName() string {
	return "earning_events"
}

// EarningWithdrawal is one user-authorized CNX payout request.
type EarningWithdrawal struct {
	ID                      uint                    `json:"id" gorm:"primarykey;index:idx_earning_withdrawals_status_id,priority:2"`
	CreatedAt               time.Time               `json:"created_at" gorm:"not null;index:idx_earning_withdrawals_user_network_created,priority:3;index:idx_earning_withdrawals_benefit_network_created,priority:3"`
	UpdatedAt               time.Time               `json:"updated_at" gorm:"not null"`
	UserID                  uint                    `json:"user_id" gorm:"not null;index:idx_earning_withdrawals_user_network_created,priority:1"`
	BenefitAddress          string                  `json:"benefit_address" gorm:"type:string;size:42;not null;index:idx_earning_withdrawals_benefit_network_created,priority:1"`
	Amount                  BigInt                  `json:"amount" gorm:"type:string;size:255;not null"`
	WithdrawalFee           BigInt                  `json:"withdrawal_fee" gorm:"type:string;size:255;not null"`
	Network                 string                  `json:"network" gorm:"type:string;size:64;not null;index:idx_earning_withdrawals_user_network_created,priority:2;index:idx_earning_withdrawals_benefit_network_created,priority:2"`
	Status                  EarningWithdrawalStatus `json:"status" gorm:"not null;default:0;index:idx_earning_withdrawals_status_id,priority:1"`
	EarningEventID          *uint                   `json:"earning_event_id" gorm:"uniqueIndex"`
	AuthorizationTimestamp  int64                   `json:"authorization_timestamp" gorm:"not null"`
	AuthorizationSignature  string                  `json:"authorization_signature" gorm:"type:string;size:132;not null"`
	AuthorizationFingerprint string                 `json:"authorization_fingerprint" gorm:"type:string;size:64;not null;uniqueIndex"`
	TxHash                  *string                 `json:"tx_hash" gorm:"type:string;size:66"`
}

func (EarningWithdrawal) TableName() string {
	return "earning_withdrawals"
}
