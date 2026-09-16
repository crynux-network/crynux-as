package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type projectUsageSummaryForM20260918 struct {
	LastRequestAt   *int64 `gorm:"column:last_request_at"`
	RequestCountDay uint64 `gorm:"column:request_count_day;not null;default:0"`
	SuccessCountDay uint64 `gorm:"column:success_count_day;not null;default:0"`
	FailureCountDay uint64 `gorm:"column:failure_count_day;not null;default:0"`
	CreditsDay      string `gorm:"column:credits_day;type:string;size:255;not null;default:0"`
}

func (projectUsageSummaryForM20260918) TableName() string {
	return "projects"
}

func M20260918(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260918",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				project := &projectUsageSummaryForM20260918{}
				if err := m.AddColumn(project, "LastRequestAt"); err != nil {
					return err
				}
				if err := m.AddColumn(project, "RequestCountDay"); err != nil {
					return err
				}
				if err := m.AddColumn(project, "SuccessCountDay"); err != nil {
					return err
				}
				if err := m.AddColumn(project, "FailureCountDay"); err != nil {
					return err
				}
				return m.AddColumn(project, "CreditsDay")
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				project := &projectUsageSummaryForM20260918{}
				if err := m.DropColumn(project, "CreditsDay"); err != nil {
					return err
				}
				if err := m.DropColumn(project, "FailureCountDay"); err != nil {
					return err
				}
				if err := m.DropColumn(project, "SuccessCountDay"); err != nil {
					return err
				}
				if err := m.DropColumn(project, "RequestCountDay"); err != nil {
					return err
				}
				return m.DropColumn(project, "LastRequestAt")
			},
		},
	})
}
