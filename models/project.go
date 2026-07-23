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
	Status        ProjectStatus `json:"status" gorm:"not null;default:0;index"`
}
