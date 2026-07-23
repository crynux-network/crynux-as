package models

import "time"

type DepositStatus int8

const (
	DepositStatusPending DepositStatus = iota
	DepositStatusProcessed
	DepositStatusInvalid
)

type Deposit struct {
	ID            uint          `json:"id" gorm:"primarykey"`
	CreatedAt     time.Time     `json:"created_at" gorm:"not null"`
	UpdatedAt     time.Time     `json:"updated_at" gorm:"not null"`
	UserID        uint          `json:"user_id" gorm:"not null;index"`
	Network       string        `json:"network" gorm:"type:string;size:64;not null;uniqueIndex:idx_deposits_network_tx_log"`
	Token         string        `json:"token" gorm:"type:string;size:64;not null"`
	TxHash        string        `json:"tx_hash" gorm:"type:string;size:128;not null;uniqueIndex:idx_deposits_network_tx_log"`
	LogIndex      uint          `json:"log_index" gorm:"not null;uniqueIndex:idx_deposits_network_tx_log"`
	FromAddress   string        `json:"from_address" gorm:"type:string;size:64;not null;index"`
	Amount        BigInt        `json:"amount" gorm:"type:string;size:255;not null"`
	Credits       BigInt        `json:"credits" gorm:"type:string;size:255;not null"`
	Status        DepositStatus `json:"status" gorm:"not null;default:0;index"`
	CreditEventID uint          `json:"credit_event_id" gorm:"not null;default:0;index"`
}
