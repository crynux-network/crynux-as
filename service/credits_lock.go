package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"fmt"
	"math/big"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AvailableCredits returns balance - locked. Negative results are clamped to zero.
func AvailableCredits(balance, locked *big.Int) *big.Int {
	b := big.NewInt(0)
	if balance != nil {
		b = new(big.Int).Set(balance)
	}
	l := big.NewInt(0)
	if locked != nil {
		l = locked
	}
	available := new(big.Int).Sub(b, l)
	if available.Sign() < 0 {
		return big.NewInt(0)
	}
	return available
}

// EnsureSufficientAvailable returns ErrInsufficientBalance when available Credits
// (balance - locked) are strictly below required.
func EnsureSufficientAvailable(balance, locked, required *big.Int) error {
	return EnsureSufficientBalance(AvailableCredits(balance, locked), required)
}

func reserveCreditsTx(tx *gorm.DB, userID uint, amount *big.Int) error {
	if amount == nil || amount.Sign() <= 0 {
		return errors.New("credits lock amount must be positive")
	}
	var account models.CreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		First(&account).Error; err != nil {
		return err
	}
	if err := EnsureSufficientAvailable(&account.Balance.Int, &account.Locked.Int, amount); err != nil {
		return err
	}
	account.Locked = models.BigInt{Int: *new(big.Int).Add(&account.Locked.Int, amount)}
	return tx.Save(&account).Error
}

// releaseCreditsHoldTx releases a job's Credits hold when credits_lock_status is held.
// It is idempotent for none and released. Unlock authority is the job columns, not billing_data.
func releaseCreditsHoldTx(tx *gorm.DB, job *models.TaskJob) error {
	if job == nil {
		return errors.New("job is required")
	}
	if job.CreditsLockStatus != models.TaskJobCreditsLockHeld {
		return nil
	}
	var account models.CreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", job.UserID).
		First(&account).Error; err != nil {
		return err
	}
	hold := new(big.Int).Set(&job.CreditsLocked.Int)
	if hold.Sign() < 0 {
		return fmt.Errorf("job %d credits_locked must be non-negative", job.ID)
	}
	if account.Locked.Cmp(hold) < 0 {
		log.Errorf(
			"credits locked underflow on release: user_id=%d job_id=%d locked=%s hold=%s",
			job.UserID, job.ID, account.Locked.String(), hold.String(),
		)
		account.Locked = models.BigInt{Int: *big.NewInt(0)}
	} else {
		account.Locked = models.BigInt{Int: *new(big.Int).Sub(&account.Locked.Int, hold)}
	}
	if err := tx.Save(&account).Error; err != nil {
		return err
	}
	job.CreditsLockStatus = models.TaskJobCreditsLockReleased
	return tx.Model(&models.TaskJob{}).Where("id = ?", job.ID).Update(
		"credits_lock_status", models.TaskJobCreditsLockReleased,
	).Error
}

type heldLockRow struct {
	UserID        uint
	CreditsLocked models.BigInt
}

// ReconcileCreditsLocked sets each credit_accounts.locked to the sum of held
// task_jobs.credits_locked for that user when they differ, and logs an error.
func ReconcileCreditsLocked(ctx context.Context, db *gorm.DB) (int, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var rows []heldLockRow
	if err := db.WithContext(dbCtx).Model(&models.TaskJob{}).
		Select("user_id", "credits_locked").
		Where("credits_lock_status = ?", models.TaskJobCreditsLockHeld).
		Find(&rows).Error; err != nil {
		return 0, err
	}

	heldByUser := make(map[uint]*big.Int)
	for _, row := range rows {
		sum := heldByUser[row.UserID]
		if sum == nil {
			sum = big.NewInt(0)
			heldByUser[row.UserID] = sum
		}
		sum.Add(sum, &row.CreditsLocked.Int)
	}

	var accounts []models.CreditAccount
	if err := db.WithContext(dbCtx).
		Where("locked != ?", "0").
		Find(&accounts).Error; err != nil {
		return 0, err
	}
	userIDs := make(map[uint]struct{}, len(heldByUser)+len(accounts))
	for userID := range heldByUser {
		userIDs[userID] = struct{}{}
	}
	for _, account := range accounts {
		userIDs[account.UserID] = struct{}{}
	}

	corrected := 0
	for userID := range userIDs {
		expected := heldByUser[userID]
		if expected == nil {
			expected = big.NewInt(0)
		}
		err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
			var account models.CreditAccount
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ?", userID).
				First(&account).Error; err != nil {
				return err
			}
			if account.Locked.Cmp(expected) == 0 {
				return nil
			}
			log.Errorf(
				"credits locked mismatch: user_id=%d account_locked=%s held_jobs_sum=%s; correcting",
				userID, account.Locked.String(), expected.String(),
			)
			account.Locked = models.BigInt{Int: *new(big.Int).Set(expected)}
			if err := tx.Save(&account).Error; err != nil {
				return err
			}
			corrected++
			return nil
		})
		if err != nil {
			return corrected, err
		}
	}
	return corrected, nil
}
