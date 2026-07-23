package models

import "time"

type User struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"not null"`
	Address   string    `json:"address" gorm:"type:string;size:64;not null;uniqueIndex"`
}
