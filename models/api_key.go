package models

import "time"

type APIKeyStatus int8

const (
	APIKeyStatusActive APIKeyStatus = iota
	APIKeyStatusDisabled
)

type APIKey struct {
	ID        uint         `json:"id" gorm:"primarykey"`
	CreatedAt time.Time    `json:"created_at" gorm:"not null"`
	UpdatedAt time.Time    `json:"updated_at" gorm:"not null"`
	ProjectID uint         `json:"project_id" gorm:"not null;index"`
	KeyHash   string       `json:"-" gorm:"type:string;size:128;not null;uniqueIndex"`
	Prefix    string       `json:"prefix" gorm:"type:string;size:16;not null"`
	Status    APIKeyStatus `json:"status" gorm:"not null;default:0;index"`
}
