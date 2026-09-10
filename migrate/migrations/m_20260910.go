package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmJobForM20260910 struct {
	ID                    uint      `gorm:"primarykey"`
	CreatedAt             time.Time `gorm:"not null;index:idx_llm_jobs_project_status_created,priority:3"`
	ProjectID             uint      `gorm:"not null;index:idx_llm_jobs_project_status_created,priority:1;index:idx_llm_jobs_project_public_id,priority:1"`
	PublicID              string    `gorm:"type:string;size:128;index:idx_llm_jobs_project_public_id,priority:2"`
	Status                int8      `gorm:"not null;default:0;index:idx_llm_jobs_project_status_created,priority:2;index:idx_llm_jobs_terminal_cleanup,priority:1"`
	BillingStatus         int8      `gorm:"not null;default:0;index:idx_llm_jobs_terminal_cleanup,priority:2"`
	CompletedAt           *time.Time `gorm:"index:idx_llm_jobs_terminal_cleanup,priority:3"`
	PromptTokens          uint64
	CompletionTokens      uint64
	TotalTokens           uint64
}

func (llmJobForM20260910) TableName() string {
	return "llm_jobs"
}

type llmCallRecordForM20260910 struct {
	ID                    uint      `gorm:"primarykey"`
	CreatedAt             time.Time `gorm:"not null;index:idx_llm_call_records_project_created,priority:2"`
	ProjectID             uint      `gorm:"not null;index:idx_llm_call_records_project_created,priority:1"`
	Credits               string    `gorm:"type:string;size:255"`
	ConstantSeconds       *float64
	SecondsPerInputToken  *float64
	SecondsPerOutputToken *float64
	ReferencePriorityGwei *string   `gorm:"type:string;size:255"`
	CreditsPerGwei        *uint64
}

func (llmCallRecordForM20260910) TableName() string {
	return "llm_call_records"
}

type creditEventForM20260910 struct {
	ID     uint `gorm:"primarykey;index:idx_credit_events_user_type_status_id,priority:4"`
	UserID uint `gorm:"not null;index:idx_credit_events_user_type_status_id,priority:1"`
	Type   int8 `gorm:"not null;index:idx_credit_events_user_type_status_id,priority:2"`
	Status int8 `gorm:"not null;default:0;index:idx_credit_events_user_type_status_id,priority:3"`
}

func (creditEventForM20260910) TableName() string {
	return "credit_events"
}

func M20260910(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260910",
			Migrate: func(tx *gorm.DB) error {
				m := tx.Migrator()
				job := &llmJobForM20260910{}
				if err := m.DropColumn(job, "PromptTokens"); err != nil {
					return err
				}
				if err := m.DropColumn(job, "CompletionTokens"); err != nil {
					return err
				}
				if err := m.DropColumn(job, "TotalTokens"); err != nil {
					return err
				}
				if err := m.CreateIndex(job, "idx_llm_jobs_terminal_cleanup"); err != nil {
					return err
				}
				if err := m.CreateIndex(job, "idx_llm_jobs_project_status_created"); err != nil {
					return err
				}
				if err := m.CreateIndex(job, "idx_llm_jobs_project_public_id"); err != nil {
					return err
				}

				callRecord := &llmCallRecordForM20260910{}
				if err := m.DropColumn(callRecord, "Credits"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "ConstantSeconds"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "SecondsPerInputToken"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "SecondsPerOutputToken"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "ReferencePriorityGwei"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "CreditsPerGwei"); err != nil {
					return err
				}
				if err := m.CreateIndex(callRecord, "idx_llm_call_records_project_created"); err != nil {
					return err
				}

				event := &creditEventForM20260910{}
				return m.CreateIndex(event, "idx_credit_events_user_type_status_id")
			},
			Rollback: func(tx *gorm.DB) error {
				m := tx.Migrator()
				event := &creditEventForM20260910{}
				if err := m.DropIndex(event, "idx_credit_events_user_type_status_id"); err != nil {
					return err
				}

				callRecord := &llmCallRecordForM20260910{}
				if err := m.DropIndex(callRecord, "idx_llm_call_records_project_created"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "CreditsPerGwei"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "ReferencePriorityGwei"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "SecondsPerOutputToken"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "SecondsPerInputToken"); err != nil {
					return err
				}
				if err := m.DropColumn(callRecord, "ConstantSeconds"); err != nil {
					return err
				}
				if err := m.AddColumn(callRecord, "Credits"); err != nil {
					return err
				}

				job := &llmJobForM20260910{}
				if err := m.DropIndex(job, "idx_llm_jobs_project_public_id"); err != nil {
					return err
				}
				if err := m.DropIndex(job, "idx_llm_jobs_project_status_created"); err != nil {
					return err
				}
				if err := m.DropIndex(job, "idx_llm_jobs_terminal_cleanup"); err != nil {
					return err
				}
				if err := m.AddColumn(job, "TotalTokens"); err != nil {
					return err
				}
				if err := m.AddColumn(job, "CompletionTokens"); err != nil {
					return err
				}
				return m.AddColumn(job, "PromptTokens")
			},
		},
	})
}
