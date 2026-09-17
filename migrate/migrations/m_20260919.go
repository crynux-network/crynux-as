package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type projectCostLevelForM20260919 struct {
	CostLevelMode        string  `gorm:"column:cost_level_mode;type:string;size:16;not null;default:static"`
	AutoQueuePosition    *int    `gorm:"column:auto_queue_position"`
	AutoMaxPriorityGwei  *string `gorm:"column:auto_max_priority_gwei;type:string;size:255"`
}

func (projectCostLevelForM20260919) TableName() string {
	return "projects"
}

type projectCostLevelModeBackfillForM20260919 struct {
	ID            uint   `gorm:"primarykey"`
	CostLevelMode string `gorm:"column:cost_level_mode;type:string;size:16;not null;default:static"`
}

func (projectCostLevelModeBackfillForM20260919) TableName() string {
	return "projects"
}

func M20260919(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260919",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				project := &projectCostLevelForM20260919{}
				if err := m.AddColumn(project, "CostLevelMode"); err != nil {
					return err
				}
				if err := m.AddColumn(project, "AutoQueuePosition"); err != nil {
					return err
				}
				if err := m.AddColumn(project, "AutoMaxPriorityGwei"); err != nil {
					return err
				}
				return tx.Model(&projectCostLevelModeBackfillForM20260919{}).
					Where("cost_level_mode <> ?", "static").
					Update("cost_level_mode", "static").Error
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				project := &projectCostLevelForM20260919{}
				if err := m.DropColumn(project, "AutoMaxPriorityGwei"); err != nil {
					return err
				}
				if err := m.DropColumn(project, "AutoQueuePosition"); err != nil {
					return err
				}
				return m.DropColumn(project, "CostLevelMode")
			},
		},
	})
}
