package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type llmJobForM20260826 struct {
	ID                   uint       `gorm:"primarykey"`
	CreatedAt            time.Time  `gorm:"not null;index"`
	UpdatedAt            time.Time  `gorm:"not null"`
	ProjectID            uint       `gorm:"not null;index"`
	PublicID             string     `gorm:"type:string;size:128;index"`
	APIType              int8       `gorm:"not null"`
	Model                string     `gorm:"type:string;size:255;not null"`
	BilledVram           uint64     `gorm:"not null;default:0"`
	Background           bool       `gorm:"not null;default:false"`
	Stream               bool       `gorm:"not null;default:false"`
	RequestBody          []byte     `gorm:"type:longblob"`
	TaskArgsJSON         string     `gorm:"type:longtext;not null"`
	BridgeClientTaskID   *uint      `gorm:"index"`
	Status               int8       `gorm:"not null;default:0;index"`
	RawResultJSON        *string    `gorm:"type:longtext"`
	FormattedResultJSON  *string    `gorm:"type:longtext"`
	PromptTokens         uint64     `gorm:"not null;default:0"`
	CompletionTokens     uint64     `gorm:"not null;default:0"`
	TotalTokens          uint64     `gorm:"not null;default:0"`
	ErrorMessage         *string    `gorm:"type:text"`
	BillingStatus        int8       `gorm:"not null;default:0;index"`
	LLMCallRecordID      *uint      `gorm:"index"`
	TaskFeeGwei          *string    `gorm:"type:string;size:255"`
	MedianPriorityGwei   *string    `gorm:"type:string;size:255"`
	EstimatedNodeSeconds *float64
	VramWeight           *float64
	StartedAt            *time.Time
	CompletedAt          *time.Time
}

func (llmJobForM20260826) TableName() string {
	return "llm_jobs"
}

func M20260826(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260826",
			Migrate: func(tx *gorm.DB) error {
				return tx.Migrator().CreateTable(&llmJobForM20260826{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable(&llmJobForM20260826{})
			},
		},
	})
}
