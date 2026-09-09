package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"fmt"
	"math/big"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ConvertTokenAmountToCredits converts a raw ERC20 amount to Credits:
// credits = amount * creditsPerToken / 10^decimals
func ConvertTokenAmountToCredits(amount *big.Int, decimals uint8, creditsPerToken uint64) (*big.Int, error) {
	if amount == nil || amount.Sign() <= 0 {
		return nil, errors.New("token amount must be positive")
	}
	if creditsPerToken == 0 {
		return nil, errors.New("credits_per_token must be positive")
	}

	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	credits := new(big.Int).Mul(amount, new(big.Int).SetUint64(creditsPerToken))
	credits.Div(credits, divisor)
	return credits, nil
}

type ProcessDepositInput struct {
	UserID      uint
	Network     string
	Token       string
	TxHash      string
	LogIndex    uint
	FromAddress string
	Amount      *big.Int
	Credits     *big.Int
}

// ProcessDeposit records a deposit and applies the Credits ledger update in one
// transaction. Duplicate (network, tx_hash, log_index) inserts are treated as
// success so re-scanning the same log is idempotent.
func ProcessDeposit(ctx context.Context, db *gorm.DB, in ProcessDepositInput) error {
	if in.UserID == 0 {
		return errors.New("user id is required")
	}
	if in.Amount == nil || in.Amount.Sign() <= 0 {
		return errors.New("deposit amount must be positive")
	}
	if in.Credits == nil || in.Credits.Sign() <= 0 {
		return errors.New("deposit credits must be positive")
	}

	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		deposit := models.Deposit{
			UserID:      in.UserID,
			Network:     in.Network,
			Token:       in.Token,
			TxHash:      in.TxHash,
			LogIndex:    in.LogIndex,
			FromAddress: in.FromAddress,
			Amount:      models.BigInt{Int: *new(big.Int).Set(in.Amount)},
			Credits:     models.BigInt{Int: *new(big.Int).Set(in.Credits)},
			Status:      models.DepositStatusProcessed,
		}
		if err := tx.Create(&deposit).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return nil
			}
			return err
		}

		event := models.CreditEvent{
			UserID: in.UserID,
			Amount: models.BigInt{Int: *new(big.Int).Set(in.Credits)},
			Type:   models.CreditEventTypeDeposit,
			RefID:  deposit.ID,
			Status: models.CreditEventStatusProcessed,
		}
		if err := tx.Create(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return fmt.Errorf("credit event already exists for deposit %d", deposit.ID)
			}
			return err
		}

		var account models.CreditAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", in.UserID).
			First(&account).Error; err != nil {
			return err
		}
		newBalance := new(big.Int).Add(&account.Balance.Int, in.Credits)
		account.Balance = models.BigInt{Int: *newBalance}
		return tx.Save(&account).Error
	})
}
