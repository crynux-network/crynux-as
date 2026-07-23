package migrations

import (
	"time"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type userForM20260723 struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
	Address   string    `gorm:"type:string;size:64;not null;uniqueIndex"`
}

func (userForM20260723) TableName() string {
	return "users"
}

type creditAccountForM20260723 struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
	UserID    uint      `gorm:"not null;uniqueIndex"`
	Balance   string    `gorm:"type:string;size:255;not null"`
}

func (creditAccountForM20260723) TableName() string {
	return "credit_accounts"
}

type creditEventForM20260723 struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"not null"`
	UserID    uint      `gorm:"not null;index"`
	Amount    string    `gorm:"type:string;size:255;not null"`
	Type      int8      `gorm:"not null;uniqueIndex:idx_credit_events_type_ref"`
	RefID     uint      `gorm:"not null;uniqueIndex:idx_credit_events_type_ref"`
	Status    int8      `gorm:"not null;default:0;index"`
}

func (creditEventForM20260723) TableName() string {
	return "credit_events"
}

type depositForM20260723 struct {
	ID            uint      `gorm:"primarykey"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
	UserID        uint      `gorm:"not null;index"`
	Network       string    `gorm:"type:string;size:64;not null;uniqueIndex:idx_deposits_network_tx_log"`
	Token         string    `gorm:"type:string;size:64;not null"`
	TxHash        string    `gorm:"type:string;size:128;not null;uniqueIndex:idx_deposits_network_tx_log"`
	LogIndex      uint      `gorm:"not null;uniqueIndex:idx_deposits_network_tx_log"`
	FromAddress   string    `gorm:"type:string;size:64;not null;index"`
	Amount        string    `gorm:"type:string;size:255;not null"`
	Credits       string    `gorm:"type:string;size:255;not null"`
	Status        int8      `gorm:"not null;default:0;index"`
}

func (depositForM20260723) TableName() string {
	return "deposits"
}

type blockchainCursorForM20260723 struct {
	ID             uint      `gorm:"primarykey"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
	Network        string    `gorm:"type:string;size:64;not null;uniqueIndex"`
	LastBlockNum   uint64    `gorm:"not null;default:0"`
	LastUpdateTime time.Time `gorm:"not null"`
}

func (blockchainCursorForM20260723) TableName() string {
	return "blockchain_cursors"
}

type projectForM20260723 struct {
	ID            uint      `gorm:"primarykey"`
	CreatedAt     time.Time `gorm:"not null"`
	UpdatedAt     time.Time `gorm:"not null"`
	UserID        uint      `gorm:"not null;index"`
	Name          string    `gorm:"type:string;size:255;not null"`
	EndpointToken string    `gorm:"type:string;size:128;not null;uniqueIndex"`
	Status        int8      `gorm:"not null;default:0;index"`
}

func (projectForM20260723) TableName() string {
	return "projects"
}

type apiKeyForM20260723 struct {
	ID        uint      `gorm:"primarykey"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
	ProjectID uint      `gorm:"not null;index"`
	KeyHash   string    `gorm:"type:string;size:128;not null;uniqueIndex"`
	Prefix    string    `gorm:"type:string;size:16;not null"`
	Status    int8      `gorm:"not null;default:0;index"`
}

func (apiKeyForM20260723) TableName() string {
	return "api_keys"
}

type llmCallRecordForM20260723 struct {
	ID               uint      `gorm:"primarykey"`
	CreatedAt        time.Time `gorm:"not null;index"`
	ProjectID        uint      `gorm:"not null;index"`
	APIKeyID         uint      `gorm:"not null;index"`
	Model            string    `gorm:"type:string;size:255;not null"`
	PromptTokens     uint64    `gorm:"not null;default:0"`
	CompletionTokens uint64    `gorm:"not null;default:0"`
	TotalTokens      uint64    `gorm:"not null;default:0"`
	Status           int8      `gorm:"not null;default:0;index"`
	Credits          string    `gorm:"type:string;size:255;not null"`
	DurationMs       uint64    `gorm:"not null;default:0"`
}

func (llmCallRecordForM20260723) TableName() string {
	return "llm_call_records"
}

type projectUsageStatForM20260723 struct {
	ID               uint      `gorm:"primarykey"`
	CreatedAt        time.Time `gorm:"not null"`
	UpdatedAt        time.Time `gorm:"not null"`
	ProjectID        uint      `gorm:"not null;uniqueIndex:idx_project_usage_stats_project_period"`
	PeriodStart      time.Time `gorm:"not null;uniqueIndex:idx_project_usage_stats_project_period;index"`
	CallCount        uint64    `gorm:"not null;default:0"`
	SuccessCount     uint64    `gorm:"not null;default:0"`
	FailureCount     uint64    `gorm:"not null;default:0"`
	PromptTokens     uint64    `gorm:"not null;default:0"`
	CompletionTokens uint64    `gorm:"not null;default:0"`
	TotalTokens      uint64    `gorm:"not null;default:0"`
	Credits          string    `gorm:"type:string;size:255;not null"`
}

func (projectUsageStatForM20260723) TableName() string {
	return "project_usage_stats"
}

func M20260723(db *gorm.DB) *gormigrate.Gormigrate {
	return gormigrate.New(db, gormigrate.DefaultOptions, []*gormigrate.Migration{
		{
			ID: "M20260723",
			Migrate: func(tx *gorm.DB) error {
				return tx.Migrator().CreateTable(
					&userForM20260723{},
					&creditAccountForM20260723{},
					&creditEventForM20260723{},
					&depositForM20260723{},
					&blockchainCursorForM20260723{},
					&projectForM20260723{},
					&apiKeyForM20260723{},
					&llmCallRecordForM20260723{},
					&projectUsageStatForM20260723{},
				)
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable(
					&projectUsageStatForM20260723{},
					&llmCallRecordForM20260723{},
					&apiKeyForM20260723{},
					&projectForM20260723{},
					&blockchainCursorForM20260723{},
					&depositForM20260723{},
					&creditEventForM20260723{},
					&creditAccountForM20260723{},
					&userForM20260723{},
				)
			},
		},
	})
}
