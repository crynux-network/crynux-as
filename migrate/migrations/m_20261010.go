package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type creditAccountLockedForM20261010 struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
	UserID    uint      `gorm:"not null;uniqueIndex"`
	Balance   string    `gorm:"type:string;size:255;not null"`
	Locked    string    `gorm:"type:string;size:255;not null;default:0"`
}

func (creditAccountLockedForM20261010) TableName() string {
	return "credit_accounts"
}

type taskJobCreditsLockForM20261010 struct {
	ID                uint      `gorm:"primarykey"`
	CreatedAt         time.Time `gorm:"not null"`
	UpdatedAt         time.Time `gorm:"not null"`
	CreditsLocked     string    `gorm:"type:string;size:255;not null;default:0"`
	CreditsLockStatus int8      `gorm:"not null;default:0;index:idx_task_jobs_credits_lock_status"`
}

func (taskJobCreditsLockForM20261010) TableName() string {
	return "task_jobs"
}

func M20261010(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20261010",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				if err := m.AddColumn(&creditAccountLockedForM20261010{}, "Locked"); err != nil {
					return err
				}
				job := &taskJobCreditsLockForM20261010{}
				if err := m.AddColumn(job, "CreditsLocked"); err != nil {
					return err
				}
				if err := m.AddColumn(job, "CreditsLockStatus"); err != nil {
					return err
				}
				return m.CreateIndex(job, "idx_task_jobs_credits_lock_status")
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				job := &taskJobCreditsLockForM20261010{}
				if err := m.DropIndex(job, "idx_task_jobs_credits_lock_status"); err != nil {
					return err
				}
				if err := m.DropColumn(job, "CreditsLockStatus"); err != nil {
					return err
				}
				if err := m.DropColumn(job, "CreditsLocked"); err != nil {
					return err
				}
				return m.DropColumn(&creditAccountLockedForM20261010{}, "Locked")
			},
		},
	})
}
