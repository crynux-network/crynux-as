package migrations

import (
	"crynux_as/models"
	"errors"
	"math/big"
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type openingBalanceAccount struct {
	UserID  uint
	Balance string
}

// Frozen task_jobs schema as created by M20261008.
type taskJobForM20261008 struct {
	ID                 uint      `gorm:"primarykey"`
	CreatedAt          time.Time `gorm:"not null;index:idx_task_jobs_project_status_created,priority:3;index"`
	UpdatedAt          time.Time `gorm:"not null"`
	ProjectID          uint      `gorm:"not null;index:idx_task_jobs_project_status_created,priority:1;index:idx_task_jobs_project_public_id,priority:1;index"`
	UserID             uint      `gorm:"not null;index"`
	PriorityGwei       string    `gorm:"type:string;size:255;not null"`
	TaskType           int8      `gorm:"not null"`
	APIType            int8      `gorm:"not null"`
	Model              string    `gorm:"type:string;size:255;not null"`
	BilledVram         uint64    `gorm:"not null;default:0"`
	PublicID           string    `gorm:"type:string;size:128;index:idx_task_jobs_project_public_id,priority:2;index"`
	Background         bool      `gorm:"not null;default:false"`
	Stream             bool      `gorm:"not null;default:false"`
	TaskArgsJSON       string    `gorm:"type:longtext;not null"`
	TaskVersion        *string   `gorm:"type:string;size:64"`
	MinVram            *uint64
	RequiredGPU        string     `gorm:"type:string;size:255"`
	RequiredGPUVram    uint64     `gorm:"not null;default:0"`
	RepeatNum          uint64     `gorm:"not null;default:1"`
	BridgeClientTaskID *uint      `gorm:"index"`
	Status             int8       `gorm:"not null;default:0;index:idx_task_jobs_project_status_created,priority:2;index:idx_task_jobs_terminal_cleanup,priority:1;index"`
	RawResultJSON      *string    `gorm:"type:longtext"`
	FormattedResultJSON *string   `gorm:"type:longtext"`
	ErrorMessage       *string    `gorm:"type:text"`
	BillingStatus      int8       `gorm:"not null;default:0;index:idx_task_jobs_terminal_cleanup,priority:2;index"`
	TaskCallRecordID   *uint      `gorm:"index"`
	BillingData        string     `gorm:"type:longtext;not null"`
	StartedAt          *time.Time
	CompletedAt        *time.Time `gorm:"index:idx_task_jobs_terminal_cleanup,priority:3"`
}

func (taskJobForM20261008) TableName() string {
	return "task_jobs"
}

type taskCallRecordForM20261008 struct {
	ID                   uint      `gorm:"primarykey"`
	CreatedAt            time.Time `gorm:"not null;index:idx_task_call_records_project_created,priority:2;index"`
	UserID               uint      `gorm:"not null;index"`
	ProjectID            uint      `gorm:"not null;index:idx_task_call_records_project_created,priority:1;index"`
	TaskJobID            *uint     `gorm:"uniqueIndex"`
	TaskType             int8      `gorm:"not null"`
	APIType              int8      `gorm:"not null"`
	Model                string    `gorm:"type:string;size:255;not null"`
	PromptTokens         uint64    `gorm:"not null;default:0"`
	CompletionTokens     uint64    `gorm:"not null;default:0"`
	TotalTokens          uint64    `gorm:"not null;default:0"`
	PriorityGwei         string    `gorm:"type:string;size:255;not null"`
	TokenUsageApplicable int8      `gorm:"not null"`
	Status               int8      `gorm:"not null;default:0;index"`
	AcceptedAt           time.Time `gorm:"not null;index"`
	CompletedAt          time.Time `gorm:"not null;index"`
	DurationMs           uint64    `gorm:"not null;default:0"`
	BilledVram           uint64    `gorm:"not null;default:0"`
}

func (taskCallRecordForM20261008) TableName() string {
	return "task_call_records"
}

func M20261008(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20261008",
			Migrate: func(tx *gorm.DB) error {
				var accounts []openingBalanceAccount
				if err := tx.Table("credit_accounts").Select("user_id", "balance").Find(&accounts).Error; err != nil {
					return err
				}
				for _, account := range accounts {
					balance, ok := new(big.Int).SetString(account.Balance, 10)
					if !ok {
						return errors.New("credit account balance is not a decimal integer")
					}
					if balance.Sign() < 0 {
						return errors.New("credit account balance must not be negative")
					}
				}

				clearTables := []string{
					"credit_events",
					"deposits",
					"account_usage_hourly_stats",
					"project_usage_hourly_stats",
					"project_model_usage_10m_stats",
					"project_duration_usage_10m_stats",
					"project_model_usage_snapshots",
					"project_duration_histogram_snapshots",
					"usage_stats_progress",
				}
				for _, table := range clearTables {
					if tx.Migrator().HasTable(table) {
						if err := tx.Exec("DELETE FROM " + table).Error; err != nil {
							return err
						}
					}
				}
				for _, account := range accounts {
					balance, _ := new(big.Int).SetString(account.Balance, 10)
					if balance.Sign() == 0 {
						continue
					}
					event := models.CreditEvent{
						UserID: account.UserID,
						Amount: models.BigInt{Int: *balance},
						Type:   models.CreditEventTypeOpeningBalance,
						RefID:  account.UserID,
						Status: models.CreditEventStatusProcessed,
					}
					if err := tx.Create(&event).Error; err != nil {
						return err
					}
				}
				if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Table("projects").Updates(map[string]interface{}{
					"last_request_at":             nil,
					"request_count_day":           0,
					"success_count_day":           0,
					"failure_count_day":           0,
					"credits_day":                 "0",
					"recent_window_success_count": 0,
					"recent_window_failure_count": 0,
				}).Error; err != nil {
					return err
				}
				if tx.Migrator().HasTable("llm_jobs") {
					if err := tx.Migrator().DropTable("llm_jobs"); err != nil {
						return err
					}
				}
				if tx.Migrator().HasTable("llm_call_records") {
					if err := tx.Migrator().DropTable("llm_call_records"); err != nil {
						return err
					}
				}
				return tx.AutoMigrate(&taskJobForM20261008{}, &taskCallRecordForM20261008{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable(&taskCallRecordForM20261008{}, &taskJobForM20261008{})
			},
		},
	})
}
