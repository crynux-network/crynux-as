package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type taskJobForM20261029 struct {
	ExecutionSource  string `gorm:"type:string;size:32;not null;default:crynux;index:idx_task_jobs_provider_queue,priority:1"`
	SortablePriority string `gorm:"type:string;size:80;not null;default:0;index:idx_task_jobs_provider_queue,priority:3"`
	Status           int8   `gorm:"not null;default:0;index:idx_task_jobs_provider_queue,priority:2"`
	ID               uint   `gorm:"primarykey;index:idx_task_jobs_provider_queue,priority:4"`
}

func (taskJobForM20261029) TableName() string {
	return "task_jobs"
}

type taskCallRecordForM20261029 struct {
	ExecutionSource string `gorm:"type:string;size:32;not null;default:crynux"`
	SharedAPIKeyID  *uint  `gorm:"index:idx_task_call_records_shared_api_key_id,priority:1"`
	ID              uint   `gorm:"primarykey;index:idx_task_call_records_shared_api_key_id,priority:2"`
}

func (taskCallRecordForM20261029) TableName() string {
	return "task_call_records"
}

type sharedAPIKeyForM20261029 struct {
	ID                          uint       `gorm:"primarykey"`
	PublicID                    string     `gorm:"type:string;size:64;not null;uniqueIndex"`
	CreatedAt                   time.Time  `gorm:"not null"`
	UpdatedAt                   time.Time  `gorm:"not null"`
	UserID                      uint       `gorm:"not null;index:idx_shared_api_keys_user_id,priority:1"`
	ServiceProvider             string     `gorm:"type:string;size:32;not null;uniqueIndex:idx_shared_api_keys_provider_fingerprint,priority:1;index:idx_shared_api_keys_eligibility,priority:1"`
	MaskedSuffix                string     `gorm:"type:string;size:16;not null"`
	OwnerEnabled                bool       `gorm:"not null;default:true;index:idx_shared_api_keys_eligibility,priority:2"`
	DeletedAt                   *time.Time `gorm:"index:idx_shared_api_keys_eligibility,priority:3"`
	ConsecutiveUnavailableCount int        `gorm:"not null;default:0"`
	CooldownUntil               *time.Time `gorm:"index:idx_shared_api_keys_eligibility,priority:4"`
	Fingerprint                 *string    `gorm:"type:string;size:64;uniqueIndex:idx_shared_api_keys_provider_fingerprint,priority:2"`
	KeyCiphertext               []byte     `gorm:"type:blob"`
	KeyNonce                    []byte     `gorm:"type:blob"`
}

func (sharedAPIKeyForM20261029) TableName() string {
	return "shared_api_keys"
}

type earningAccountForM20261029 struct {
	ID               uint      `gorm:"primarykey"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
	UserID           uint      `gorm:"not null;uniqueIndex"`
	Available        string    `gorm:"type:string;size:255;not null;default:0"`
	WithdrawalLocked string    `gorm:"type:string;size:255;not null;default:0"`
}

func (earningAccountForM20261029) TableName() string {
	return "earning_accounts"
}

type earningEventForM20261029 struct {
	ID               uint       `gorm:"primarykey;index:idx_earning_events_status_id,priority:2"`
	CreatedAt        time.Time  `gorm:"not null"`
	UpdatedAt        time.Time  `gorm:"not null"`
	EarningAccountID *uint      `gorm:"index"`
	Address          string     `gorm:"type:string;size:42;not null"`
	EventType        int8       `gorm:"not null;uniqueIndex:idx_earning_events_type_source,priority:1"`
	Amount           string     `gorm:"type:string;size:255;not null"`
	SourceType       string     `gorm:"type:string;size:64;not null;uniqueIndex:idx_earning_events_type_source,priority:2"`
	SourceID         uint       `gorm:"not null;uniqueIndex:idx_earning_events_type_source,priority:3"`
	Status           int8       `gorm:"not null;default:1;index:idx_earning_events_status_id,priority:1"`
	ProcessedAt      *time.Time
	Payload          string `gorm:"type:longtext;not null"`
}

func (earningEventForM20261029) TableName() string {
	return "earning_events"
}

type earningWithdrawalForM20261029 struct {
	ID                       uint      `gorm:"primarykey;index:idx_earning_withdrawals_status_id,priority:2"`
	CreatedAt                time.Time `gorm:"not null;index:idx_earning_withdrawals_user_network_created,priority:3;index:idx_earning_withdrawals_benefit_network_created,priority:3"`
	UpdatedAt                time.Time `gorm:"not null"`
	UserID                   uint      `gorm:"not null;index:idx_earning_withdrawals_user_network_created,priority:1"`
	BenefitAddress           string    `gorm:"type:string;size:42;not null;index:idx_earning_withdrawals_benefit_network_created,priority:1"`
	Amount                   string    `gorm:"type:string;size:255;not null"`
	WithdrawalFee            string    `gorm:"type:string;size:255;not null"`
	Network                  string    `gorm:"type:string;size:64;not null;index:idx_earning_withdrawals_user_network_created,priority:2;index:idx_earning_withdrawals_benefit_network_created,priority:2"`
	Status                   int8      `gorm:"not null;default:0;index:idx_earning_withdrawals_status_id,priority:1"`
	EarningEventID           *uint     `gorm:"uniqueIndex"`
	AuthorizationTimestamp   int64     `gorm:"not null"`
	AuthorizationSignature   string    `gorm:"type:string;size:132;not null"`
	AuthorizationFingerprint string    `gorm:"type:string;size:64;not null;uniqueIndex"`
	TxHash                   *string   `gorm:"type:string;size:66"`
}

func (earningWithdrawalForM20261029) TableName() string {
	return "earning_withdrawals"
}

func M20261029(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{{
		ID: "M20261029",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Migrator().AddColumn(&taskJobForM20261029{}, "ExecutionSource"); err != nil {
				return err
			}
			if err := tx.Migrator().AddColumn(&taskJobForM20261029{}, "SortablePriority"); err != nil {
				return err
			}
			if err := tx.Migrator().CreateIndex(&taskJobForM20261029{}, "idx_task_jobs_provider_queue"); err != nil {
				return err
			}
			if err := tx.Migrator().AddColumn(&taskCallRecordForM20261029{}, "ExecutionSource"); err != nil {
				return err
			}
			if err := tx.Migrator().AddColumn(&taskCallRecordForM20261029{}, "SharedAPIKeyID"); err != nil {
				return err
			}
			if err := tx.Migrator().CreateIndex(&taskCallRecordForM20261029{}, "idx_task_call_records_shared_api_key_id"); err != nil {
				return err
			}
			if err := tx.Migrator().CreateTable(&sharedAPIKeyForM20261029{}); err != nil {
				return err
			}
			if err := tx.Migrator().CreateTable(&earningAccountForM20261029{}); err != nil {
				return err
			}
			if err := tx.Migrator().CreateTable(&earningEventForM20261029{}); err != nil {
				return err
			}
			return tx.Migrator().CreateTable(&earningWithdrawalForM20261029{})
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable(&earningWithdrawalForM20261029{}); err != nil {
				return err
			}
			if err := tx.Migrator().DropTable(&earningEventForM20261029{}); err != nil {
				return err
			}
			if err := tx.Migrator().DropTable(&earningAccountForM20261029{}); err != nil {
				return err
			}
			if err := tx.Migrator().DropTable(&sharedAPIKeyForM20261029{}); err != nil {
				return err
			}
			if err := tx.Migrator().DropIndex(&taskCallRecordForM20261029{}, "idx_task_call_records_shared_api_key_id"); err != nil {
				return err
			}
			if err := tx.Migrator().DropColumn(&taskCallRecordForM20261029{}, "SharedAPIKeyID"); err != nil {
				return err
			}
			if err := tx.Migrator().DropColumn(&taskCallRecordForM20261029{}, "ExecutionSource"); err != nil {
				return err
			}
			if err := tx.Migrator().DropIndex(&taskJobForM20261029{}, "idx_task_jobs_provider_queue"); err != nil {
				return err
			}
			if err := tx.Migrator().DropColumn(&taskJobForM20261029{}, "SortablePriority"); err != nil {
				return err
			}
			return tx.Migrator().DropColumn(&taskJobForM20261029{}, "ExecutionSource")
		},
	}})
}
