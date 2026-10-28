package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"math/big"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrEarningAccountNotFound = errors.New("earning account not found")
	ErrInsufficientEarnings   = errors.New("insufficient available earnings")
)

func EnsureEarningAccount(ctx context.Context, db *gorm.DB, userID uint) (*models.EarningAccount, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var account models.EarningAccount
	err := db.WithContext(dbCtx).Where("user_id = ?", userID).First(&account).Error
	if err == nil {
		return &account, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	account = models.EarningAccount{
		UserID:           userID,
		Available:        models.BigInt{Int: *big.NewInt(0)},
		WithdrawalLocked: models.BigInt{Int: *big.NewInt(0)},
	}
	if err := db.WithContext(dbCtx).Create(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			if err := db.WithContext(dbCtx).Where("user_id = ?", userID).First(&account).Error; err != nil {
				return nil, err
			}
			return &account, nil
		}
		return nil, err
	}
	return &account, nil
}

func GetEarningAccount(ctx context.Context, db *gorm.DB, userID uint) (*models.EarningAccount, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var account models.EarningAccount
	if err := db.WithContext(dbCtx).Where("user_id = ?", userID).First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrEarningAccountNotFound
		}
		return nil, err
	}
	return &account, nil
}

// CreateTaskIncomeEarningTx increases available balance and writes a processed TaskIncome event.
func CreateTaskIncomeEarningTx(tx *gorm.DB, userID uint, address string, amount *big.Int, callRecordID uint) error {
	if amount == nil || amount.Sign() <= 0 {
		return nil
	}
	var account models.EarningAccount
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		account = models.EarningAccount{
			UserID:           userID,
			Available:        models.BigInt{Int: *big.NewInt(0)},
			WithdrawalLocked: models.BigInt{Int: *big.NewInt(0)},
		}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&account).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	newAvailable := new(big.Int).Add(&account.Available.Int, amount)
	account.Available = models.BigInt{Int: *newAvailable}
	if err := tx.Save(&account).Error; err != nil {
		return err
	}

	now := time.Now()
	accountID := account.ID
	event := models.EarningEvent{
		EarningAccountID: &accountID,
		Address:          address,
		EventType:        models.EarningEventTypeTaskIncome,
		Amount:           models.BigInt{Int: *new(big.Int).Set(amount)},
		SourceType:       models.EarningSourceTypeTaskCallRecord,
		SourceID:         callRecordID,
		Status:           models.EarningEventStatusProcessed,
		ProcessedAt:      &now,
		Payload:          "{}",
	}
	return tx.Create(&event).Error
}

// CountActiveProviderCallsForUser returns the process-local active slot count across the owner's Shared API Keys.
func CountActiveProviderCallsForUser(ctx context.Context, db *gorm.DB, userID uint) (int64, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var keyIDs []uint
	if err := db.WithContext(dbCtx).Model(&models.SharedAPIKey{}).
		Where("user_id = ?", userID).
		Pluck("id", &keyIDs).Error; err != nil {
		return 0, err
	}
	slots := GetProviderKeySlots()
	var active int64
	for _, id := range keyIDs {
		active += int64(slots.ActiveCount(id))
	}
	return active, nil
}
