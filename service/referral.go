package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const referralCodeLength = 12

var referralCodeAlphabet = []byte("ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789")

func generateReferralCode() (string, error) {
	buf := make([]byte, referralCodeLength)
	max := big.NewInt(int64(len(referralCodeAlphabet)))
	for i := 0; i < referralCodeLength; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = referralCodeAlphabet[n.Int64()]
	}
	return string(buf), nil
}

func createActiveReferralCode(tx *gorm.DB, userID uint) (*models.ReferralCode, error) {
	for attempt := 0; attempt < 8; attempt++ {
		code, err := generateReferralCode()
		if err != nil {
			return nil, err
		}
		row := models.ReferralCode{
			UserID: userID,
			Code:   code,
			Status: models.ReferralCodeStatusActive,
		}
		if err := tx.Create(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				continue
			}
			return nil, err
		}
		return &row, nil
	}
	return nil, errors.New("failed to generate a unique referral code")
}

func creditAccountLocked(tx *gorm.DB, userID uint, amount *big.Int) error {
	if amount == nil || amount.Sign() <= 0 {
		return nil
	}
	var account models.CreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		First(&account).Error; err != nil {
		return err
	}
	account.Balance = models.BigInt{Int: *new(big.Int).Add(&account.Balance.Int, amount)}
	return tx.Save(&account).Error
}

// EnsureUserWithReferral returns the user for address, creating the account on first
// login. New users receive signup bonus, an active referral code, and optional
// invitee binding/bonus from inviteCode. Existing users ignore inviteCode.
func EnsureUserWithReferral(ctx context.Context, db *gorm.DB, address string, inviteCode string) (*models.User, error) {
	user, err := models.FindUserByAddress(ctx, db, address)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	cfg := config.GetConfig()
	signupBonus := cfg.Credits.SignupBonus
	inviteeBonus := cfg.Credits.Referral.InviteeBonus
	inviteCode = strings.TrimSpace(inviteCode)

	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var created models.User
	err = db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		created = models.User{Address: address}
		if err := tx.Create(&created).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return tx.Where("address = ?", address).First(&created).Error
			}
			return err
		}

		balance := big.NewInt(0)
		if signupBonus > 0 {
			balance = new(big.Int).SetUint64(signupBonus)
		}
		account := models.CreditAccount{
			UserID:  created.ID,
			Balance: models.BigInt{Int: *balance},
		}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}

		if signupBonus > 0 {
			event := models.CreditEvent{
				UserID: created.ID,
				Amount: models.BigInt{Int: *new(big.Int).SetUint64(signupBonus)},
				Type:   models.CreditEventTypeSignupBonus,
				RefID:  created.ID,
				Status: models.CreditEventStatusProcessed,
			}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
		}

		if _, err := createActiveReferralCode(tx, created.ID); err != nil {
			return err
		}

		if inviteCode == "" {
			return nil
		}

		var codeRow models.ReferralCode
		if err := tx.Where("code = ? AND status = ?", inviteCode, models.ReferralCodeStatusActive).
			First(&codeRow).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if codeRow.UserID == created.ID {
			return nil
		}

		binding := models.ReferralBinding{
			InviteeUserID:  created.ID,
			ReferrerUserID: codeRow.UserID,
			ReferralCodeID: codeRow.ID,
		}
		if err := tx.Create(&binding).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return nil
			}
			return err
		}

		if inviteeBonus > 0 {
			bonus := new(big.Int).SetUint64(inviteeBonus)
			event := models.CreditEvent{
				UserID: created.ID,
				Amount: models.BigInt{Int: *bonus},
				Type:   models.CreditEventTypeInviteeBonus,
				RefID:  created.ID,
				Status: models.CreditEventStatusProcessed,
			}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
			if err := creditAccountLocked(tx, created.ID, bonus); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// EnsureActiveReferralCode returns the user's active referral code, creating one when missing.
func EnsureActiveReferralCode(ctx context.Context, db *gorm.DB, userID uint) (*models.ReferralCode, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var existing models.ReferralCode
	err := db.WithContext(dbCtx).
		Where("user_id = ? AND status = ?", userID, models.ReferralCodeStatusActive).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var created *models.ReferralCode
	err = db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var again models.ReferralCode
		if err := tx.Where("user_id = ? AND status = ?", userID, models.ReferralCodeStatusActive).
			First(&again).Error; err == nil {
			created = &again
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		row, err := createActiveReferralCode(tx, userID)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ResetReferralCode revokes the active code and issues a new one.
func ResetReferralCode(ctx context.Context, db *gorm.DB, userID uint) (*models.ReferralCode, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var created *models.ReferralCode
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		if err := tx.Model(&models.ReferralCode{}).
			Where("user_id = ? AND status = ?", userID, models.ReferralCodeStatusActive).
			Updates(map[string]interface{}{
				"status":     models.ReferralCodeStatusRevoked,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		row, err := createActiveReferralCode(tx, userID)
		if err != nil {
			return err
		}
		created = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// CountInviteePurchases returns the number of deposits made by users bound to
// referrerUserID. When excludeDepositID is non-zero, that deposit row is omitted.
func CountInviteePurchases(tx *gorm.DB, referrerUserID uint, excludeDepositID uint) (int64, error) {
	inviteeIDs := tx.Model(&models.ReferralBinding{}).
		Select("invitee_user_id").
		Where("referrer_user_id = ?", referrerUserID)
	q := tx.Model(&models.Deposit{}).Where("user_id IN (?)", inviteeIDs)
	if excludeDepositID > 0 {
		q = q.Where("id != ?", excludeDepositID)
	}
	var count int64
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func applyDepositReferralReward(tx *gorm.DB, deposit *models.Deposit) error {
	var binding models.ReferralBinding
	if err := tx.Where("invitee_user_id = ?", deposit.UserID).First(&binding).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	priorCount, err := CountInviteePurchases(tx, binding.ReferrerUserID, deposit.ID)
	if err != nil {
		return err
	}
	if priorCount < 0 {
		return errors.New("invitee purchase count is negative")
	}

	ratio, err := config.GetConfig().EffectiveDepositRewardRatio(uint64(priorCount))
	if err != nil {
		return err
	}
	if ratio == nil || ratio.Sign() <= 0 {
		return nil
	}

	rewardRat := new(big.Rat).SetInt(&deposit.Credits.Int)
	rewardRat.Mul(rewardRat, ratio)
	rewardInt := new(big.Int).Div(rewardRat.Num(), rewardRat.Denom())
	if rewardInt.Sign() <= 0 {
		return nil
	}

	depositID := deposit.ID
	inviteeID := deposit.UserID
	reward := models.ReferralReward{
		ReferrerUserID: binding.ReferrerUserID,
		InviteeUserID:  &inviteeID,
		Kind:           models.ReferralRewardKindDepositReward,
		Amount:         models.BigInt{Int: *rewardInt},
		DepositID:      &depositID,
		DepositCredits: models.BigInt{Int: *new(big.Int).Set(&deposit.Credits.Int)},
	}
	if err := tx.Create(&reward).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil
		}
		return err
	}

	event := models.CreditEvent{
		UserID: binding.ReferrerUserID,
		Amount: models.BigInt{Int: *rewardInt},
		Type:   models.CreditEventTypeReferralReward,
		RefID:  reward.ID,
		Status: models.CreditEventStatusProcessed,
	}
	if err := tx.Create(&event).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return fmt.Errorf("credit event already exists for referral reward %d", reward.ID)
		}
		return err
	}
	return creditAccountLocked(tx, binding.ReferrerUserID, rewardInt)
}
