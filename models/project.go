package models

import "time"

type ProjectStatus int8

const (
	ProjectStatusActive ProjectStatus = iota
	ProjectStatusDisabled
)

type Project struct {
	ID            uint          `json:"id" gorm:"primarykey"`
	CreatedAt     time.Time     `json:"created_at" gorm:"not null"`
	UpdatedAt     time.Time     `json:"updated_at" gorm:"not null"`
	UserID        uint          `json:"user_id" gorm:"not null;index"`
	Name          string        `json:"name" gorm:"type:string;size:255;not null"`
	EndpointToken string        `json:"endpoint_token" gorm:"type:string;size:128;not null;uniqueIndex"`
	APIKeyHash    string        `json:"-" gorm:"column:api_key_hash;type:string;size:128;not null;uniqueIndex"`
	APIKeyPrefix  string        `json:"api_key_prefix" gorm:"column:api_key_prefix;type:string;size:16;not null"`
	TokenRatio    uint          `json:"token_ratio" gorm:"not null;default:10"`
	Status        ProjectStatus `json:"status" gorm:"not null;default:0;index"`
}
