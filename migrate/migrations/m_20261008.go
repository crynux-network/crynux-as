package migrations

import (
	"crynux_as/models"
	"errors"
	"math/big"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

type openingBalanceAccount struct {
	UserID  uint
	Balance string
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
				return tx.AutoMigrate(&models.TaskJob{}, &models.TaskCallRecord{})
			},
			Rollback: func(tx *gorm.DB) error {
				return tx.Migrator().DropTable(&models.TaskCallRecord{}, &models.TaskJob{})
			},
		},
	})
}
