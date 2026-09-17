package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type projectRecentFailureWindowForM20260920 struct {
	RecentWindowSuccessCount uint64 `gorm:"column:recent_window_success_count;not null;default:0"`
	RecentWindowFailureCount uint64 `gorm:"column:recent_window_failure_count;not null;default:0"`
}

func (projectRecentFailureWindowForM20260920) TableName() string {
	return "projects"
}

func M20260920(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260920",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				project := &projectRecentFailureWindowForM20260920{}
				if err := m.AddColumn(project, "RecentWindowSuccessCount"); err != nil {
					return err
				}
				return m.AddColumn(project, "RecentWindowFailureCount")
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				project := &projectRecentFailureWindowForM20260920{}
				if err := m.DropColumn(project, "RecentWindowFailureCount"); err != nil {
					return err
				}
				return m.DropColumn(project, "RecentWindowSuccessCount")
			},
		},
	})
}
