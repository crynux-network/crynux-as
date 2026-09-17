package models

import "time"

type ProjectStatus int8

const (
	ProjectStatusActive ProjectStatus = iota
	ProjectStatusDisabled
	ProjectStatusDeleted
)

const (
	CostLevelModeStatic = "static"
	CostLevelModeAuto   = "auto"
)

type Project struct {
	ID                       uint          `json:"id" gorm:"primarykey"`
	CreatedAt                time.Time     `json:"created_at" gorm:"not null"`
	UpdatedAt                time.Time     `json:"updated_at" gorm:"not null"`
	UserID                   uint          `json:"user_id" gorm:"not null;index"`
	Name                     string        `json:"name" gorm:"type:string;size:255;not null"`
	EndpointToken            string        `json:"endpoint_token" gorm:"type:string;size:128;not null;uniqueIndex"`
	APIKeyHash               string        `json:"-" gorm:"column:api_key_hash;type:string;size:128;not null;uniqueIndex"`
	APIKeyPrefix             string        `json:"api_key_prefix" gorm:"column:api_key_prefix;type:string;size:16;not null"`
	CostLevelMode            string        `json:"cost_level_mode" gorm:"type:string;size:16;not null;default:static"`
	PriorityGwei             BigInt        `json:"priority_gwei" gorm:"type:string;size:255;not null"`
	AutoQueuePosition        *int          `json:"auto_queue_position"`
	AutoMaxPriorityGwei      *BigInt       `json:"auto_max_priority_gwei" gorm:"type:string;size:255"`
	Status                   ProjectStatus `json:"status" gorm:"not null;default:0;index"`
	LastRequestAt            *int64        `json:"last_request_at"`
	RequestCountDay          uint64        `json:"request_count_day" gorm:"not null;default:0"`
	SuccessCountDay          uint64        `json:"success_count_day" gorm:"not null;default:0"`
	FailureCountDay          uint64        `json:"failure_count_day" gorm:"not null;default:0"`
	CreditsDay               BigInt        `json:"credits_day" gorm:"type:string;size:255;not null;default:0"`
	RecentWindowSuccessCount uint64        `json:"recent_window_success_count" gorm:"not null;default:0"`
	RecentWindowFailureCount uint64        `json:"recent_window_failure_count" gorm:"not null;default:0"`
}
