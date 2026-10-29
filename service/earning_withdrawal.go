package service

import (
	"context"
	"crypto/sha256"
	"crynux_as/blockchain"
	"crynux_as/config"
	"crynux_as/models"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrWithdrawalNotFound      = errors.New("withdrawal not found")
	ErrWithdrawalConflict      = errors.New("withdrawal status conflict")
	ErrWithdrawalDailyLimit    = errors.New("withdrawal daily limit exceeded")
	ErrWithdrawalNetwork       = errors.New("withdrawal network is invalid")
	ErrWithdrawalAuthFreshness = errors.New("withdrawal authorization timestamp is outside the allowed window")
	ErrWithdrawalAuthReplay    = errors.New("withdrawal authorization fingerprint already used")
	ErrWithdrawalBenefitMismatch = errors.New("benefit address mismatch")
	ErrWithdrawalBelowMinimum    = errors.New("withdrawal amount is below the network minimum")
)

func CalculateWithdrawalFee(network string, _ *big.Int) (*big.Int, error) {
	cfg := config.GetConfig()
	networkCfg, ok := cfg.Provider.Wallet.Networks[network]
	if !ok {
		return nil, ErrWithdrawalNetwork
	}
	return blockchain.ParseCNXToWei(networkCfg.FixedWithdrawalFeeCNX)
}

func BuildCanonicalWithdrawalMessage(amount *big.Int, address, benefitAddress, network string, timestamp int64) string {
	return fmt.Sprintf(
		"Crynux Relay\nAction: Withdraw %s from %s to %s on %s\nAddress: %s\nTimestamp: %d",
		amount.String(),
		address,
		benefitAddress,
		network,
		address,
		timestamp,
	)
}

func AuthorizationFingerprint(message string) string {
	sum := sha256.Sum256([]byte(message))
	return hex.EncodeToString(sum[:])
}

type PrepareWithdrawalResult struct {
	Amount          string
	WithdrawalFee   string
	Total           string
	Network         string
	ChainID         uint64
	Address         string
	BenefitAddress  string
	Timestamp       int64
	Message         string
	ExpiresAt       int64
}

func PrepareWithdrawal(ctx context.Context, db *gorm.DB, user *models.User, amount *big.Int, network string) (*PrepareWithdrawalResult, error) {
	cfg := config.GetConfig()
	networkCfg, ok := cfg.Provider.Wallet.Networks[network]
	if !ok {
		return nil, ErrWithdrawalNetwork
	}
	blockchainCfg, ok := cfg.Blockchains[network]
	if !ok {
		return nil, ErrWithdrawalNetwork
	}
	if amount == nil || amount.Sign() <= 0 {
		return nil, errors.New("amount must be positive")
	}
	minAmount, err := blockchain.ParseCNXToWei(networkCfg.WithdrawalMinCNX)
	if err != nil {
		return nil, err
	}
	if amount.Cmp(minAmount) < 0 {
		return nil, ErrWithdrawalBelowMinimum
	}
	fee, err := CalculateWithdrawalFee(network, amount)
	if err != nil {
		return nil, err
	}
	benefit, err := blockchain.GetBenefitAddress(ctx, common.HexToAddress(user.Address), network)
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	message := BuildCanonicalWithdrawalMessage(amount, user.Address, benefit.Hex(), network, now)
	total := new(big.Int).Add(amount, fee)
	_ = db
	return &PrepareWithdrawalResult{
		Amount:         amount.String(),
		WithdrawalFee:  fee.String(),
		Total:          total.String(),
		Network:        network,
		ChainID:        blockchainCfg.ChainID,
		Address:        user.Address,
		BenefitAddress: benefit.Hex(),
		Timestamp:      now,
		Message:        message,
		ExpiresAt:      now + 120,
	}, nil
}

type CreateWithdrawalInput struct {
	User            *models.User
	Amount          *big.Int
	Network         string
	BenefitAddress  string
	Timestamp       int64
	Signature       string
}

func CreateWithdrawal(ctx context.Context, db *gorm.DB, in CreateWithdrawalInput) (*models.EarningWithdrawal, error) {
	if in.User == nil {
		return nil, errors.New("user is required")
	}
	if in.Amount == nil || in.Amount.Sign() <= 0 {
		return nil, errors.New("amount must be positive")
	}
	cfg := config.GetConfig()
	networkCfg, ok := cfg.Provider.Wallet.Networks[in.Network]
	if !ok {
		return nil, ErrWithdrawalNetwork
	}
	minAmount, err := blockchain.ParseCNXToWei(networkCfg.WithdrawalMinCNX)
	if err != nil {
		return nil, err
	}
	if in.Amount.Cmp(minAmount) < 0 {
		return nil, ErrWithdrawalBelowMinimum
	}
	now := time.Now().Unix()
	if in.Timestamp < now-120 || in.Timestamp > now+60 {
		return nil, ErrWithdrawalAuthFreshness
	}
	fee, err := CalculateWithdrawalFee(in.Network, in.Amount)
	if err != nil {
		return nil, err
	}
	benefit, err := blockchain.GetBenefitAddress(ctx, common.HexToAddress(in.User.Address), in.Network)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(benefit.Hex(), in.BenefitAddress) {
		return nil, ErrWithdrawalBenefitMismatch
	}
	message := BuildCanonicalWithdrawalMessage(in.Amount, in.User.Address, benefit.Hex(), in.Network, in.Timestamp)
	verifier := blockchain.NewSignatureVerifier()
	if err := verifier.VerifySignature(message, in.Signature, in.User.Address); err != nil {
		return nil, err
	}
	fingerprint := AuthorizationFingerprint(message)
	total := new(big.Int).Add(in.Amount, fee)

	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var withdrawal models.EarningWithdrawal
	err = db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		if err := ensureWithdrawalDailyLimitsTx(tx, in.User.ID, benefit.Hex(), in.Network, networkCfg.MaxWithdrawalsPerDay); err != nil {
			return err
		}
		var account models.EarningAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", in.User.ID).First(&account).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInsufficientEarnings
			}
			return err
		}
		if account.Available.Cmp(total) < 0 {
			return ErrInsufficientEarnings
		}
		accountID := account.ID
		nowTime := time.Now()
		withdrawal = models.EarningWithdrawal{
			UserID:                   in.User.ID,
			BenefitAddress:           benefit.Hex(),
			Amount:                   models.BigInt{Int: *new(big.Int).Set(in.Amount)},
			WithdrawalFee:            models.BigInt{Int: *new(big.Int).Set(fee)},
			Network:                  in.Network,
			Status:                   models.EarningWithdrawalStatusPending,
			AuthorizationTimestamp:   in.Timestamp,
			AuthorizationSignature:   in.Signature,
			AuthorizationFingerprint: fingerprint,
		}
		if err := tx.Create(&withdrawal).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrWithdrawalAuthReplay
			}
			return err
		}
		event := models.EarningEvent{
			EarningAccountID: &accountID,
			Address:          in.User.Address,
			EventType:        models.EarningEventTypeWithdraw,
			Amount:           models.BigInt{Int: *new(big.Int).Set(total)},
			SourceType:       models.EarningSourceTypeWithdrawal,
			SourceID:         withdrawal.ID,
			Status:           models.EarningEventStatusProcessed,
			ProcessedAt:      &nowTime,
			Payload:          "{}",
		}
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		eventID := event.ID
		if err := tx.Model(&withdrawal).Update("earning_event_id", eventID).Error; err != nil {
			return err
		}
		withdrawal.EarningEventID = &eventID

		newAvailable := new(big.Int).Sub(&account.Available.Int, total)
		newLocked := new(big.Int).Add(&account.WithdrawalLocked.Int, total)
		account.Available = models.BigInt{Int: *newAvailable}
		account.WithdrawalLocked = models.BigInt{Int: *newLocked}
		return tx.Save(&account).Error
	})
	if err != nil {
		return nil, err
	}
	return &withdrawal, nil
}

func ensureWithdrawalDailyLimitsTx(tx *gorm.DB, userID uint, benefitAddress, network string, maxPerDay uint64) error {
	start := time.Now().UTC().Truncate(24 * time.Hour)
	var userCount int64
	if err := tx.Model(&models.EarningWithdrawal{}).
		Where("user_id = ? AND network = ? AND created_at >= ? AND status != ?",
			userID, network, start, models.EarningWithdrawalStatusFailed).
		Count(&userCount).Error; err != nil {
		return err
	}
	if uint64(userCount) >= maxPerDay {
		return ErrWithdrawalDailyLimit
	}
	var benefitCount int64
	if err := tx.Model(&models.EarningWithdrawal{}).
		Where("benefit_address = ? AND network = ? AND created_at >= ? AND status != ?",
			benefitAddress, network, start, models.EarningWithdrawalStatusFailed).
		Count(&benefitCount).Error; err != nil {
		return err
	}
	if uint64(benefitCount) >= maxPerDay {
		return ErrWithdrawalDailyLimit
	}
	return nil
}

func FulfillWithdrawal(ctx context.Context, db *gorm.DB, withdrawalID uint, txHash string) error {
	cfg := config.GetConfig()
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var withdrawal models.EarningWithdrawal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&withdrawal, withdrawalID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrWithdrawalNotFound
			}
			return err
		}
		if withdrawal.Status == models.EarningWithdrawalStatusSuccess {
			return nil
		}
		if withdrawal.Status != models.EarningWithdrawalStatusPending {
			return ErrWithdrawalConflict
		}
		var account models.EarningAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", withdrawal.UserID).First(&account).Error; err != nil {
			return err
		}
		total := new(big.Int).Add(&withdrawal.Amount.Int, &withdrawal.WithdrawalFee.Int)
		newLocked := new(big.Int).Sub(&account.WithdrawalLocked.Int, total)
		if newLocked.Sign() < 0 {
			return errors.New("withdrawal locked balance inconsistency")
		}
		account.WithdrawalLocked = models.BigInt{Int: *newLocked}
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		hash := txHash
		if err := tx.Model(&withdrawal).Updates(map[string]interface{}{
			"status":   models.EarningWithdrawalStatusSuccess,
			"tx_hash":  hash,
		}).Error; err != nil {
			return err
		}
		if withdrawal.WithdrawalFee.Sign() > 0 {
			networkCfg := cfg.Provider.Wallet.Networks[withdrawal.Network]
			now := time.Now()
			feeEvent := models.EarningEvent{
				EarningAccountID: nil,
				Address:          networkCfg.FeeReceiverAddress,
				EventType:        models.EarningEventTypeWithdrawalFeeIncome,
				Amount:           withdrawal.WithdrawalFee,
				SourceType:       models.EarningSourceTypeWithdrawal,
				SourceID:         withdrawal.ID,
				Status:           models.EarningEventStatusProcessed,
				ProcessedAt:      &now,
				Payload:          "{}",
			}
			if err := tx.Create(&feeEvent).Error; err != nil {
				if !errors.Is(err, gorm.ErrDuplicatedKey) {
					return err
				}
			}
		}
		return nil
	})
}

func RejectWithdrawal(ctx context.Context, db *gorm.DB, withdrawalID uint) error {
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var withdrawal models.EarningWithdrawal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&withdrawal, withdrawalID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrWithdrawalNotFound
			}
			return err
		}
		if withdrawal.Status == models.EarningWithdrawalStatusFailed {
			return nil
		}
		if withdrawal.Status != models.EarningWithdrawalStatusPending {
			return ErrWithdrawalConflict
		}
		var user models.User
		if err := tx.First(&user, withdrawal.UserID).Error; err != nil {
			return err
		}
		var account models.EarningAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", withdrawal.UserID).First(&account).Error; err != nil {
			return err
		}
		total := new(big.Int).Add(&withdrawal.Amount.Int, &withdrawal.WithdrawalFee.Int)
		newLocked := new(big.Int).Sub(&account.WithdrawalLocked.Int, total)
		if newLocked.Sign() < 0 {
			return errors.New("withdrawal locked balance inconsistency")
		}
		newAvailable := new(big.Int).Add(&account.Available.Int, total)
		account.WithdrawalLocked = models.BigInt{Int: *newLocked}
		account.Available = models.BigInt{Int: *newAvailable}
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		if err := tx.Model(&withdrawal).Update("status", models.EarningWithdrawalStatusFailed).Error; err != nil {
			return err
		}
		now := time.Now()
		accountID := account.ID
		refund := models.EarningEvent{
			EarningAccountID: &accountID,
			Address:          user.Address,
			EventType:        models.EarningEventTypeWithdrawRefund,
			Amount:           models.BigInt{Int: *total},
			SourceType:       models.EarningSourceTypeWithdrawal,
			SourceID:         withdrawal.ID,
			Status:           models.EarningEventStatusProcessed,
			ProcessedAt:      &now,
			Payload:          "{}",
		}
		if err := tx.Create(&refund).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return nil
			}
			return err
		}
		return nil
	})
}

func ListPendingWithdrawalsForWallet(ctx context.Context, db *gorm.DB, startID uint, limit int) ([]models.EarningWithdrawal, map[uint]string, error) {
	if limit <= 0 {
		limit = 100
	}
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var ids []uint
	if err := db.WithContext(dbCtx).Model(&models.EarningWithdrawal{}).
		Where("status = ? AND id > ?", models.EarningWithdrawalStatusPending, startID).
		Order("id ASC").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return nil, nil, err
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	var rows []models.EarningWithdrawal
	if err := db.WithContext(dbCtx).Where("id IN ?", ids).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	userIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		userIDs = append(userIDs, row.UserID)
	}
	var users []models.User
	if err := db.WithContext(dbCtx).Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, nil, err
	}
	addresses := make(map[uint]string, len(users))
	for _, user := range users {
		addresses[user.ID] = user.Address
	}
	return rows, addresses, nil
}

func ListProcessedEarningEventsForWallet(ctx context.Context, db *gorm.DB, startID uint, limit int) ([]models.EarningEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var events []models.EarningEvent
	err := db.WithContext(dbCtx).
		Where("status = ? AND id > ?", models.EarningEventStatusProcessed, startID).
		Order("id ASC").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	// Stop before the first unprocessed event in ID order among all events.
	if len(events) == 0 {
		return events, nil
	}
	var gap models.EarningEvent
	err = db.WithContext(dbCtx).
		Where("id > ? AND id < ? AND status != ?", startID, events[len(events)-1].ID, models.EarningEventStatusProcessed).
		Order("id ASC").
		First(&gap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return events, nil
	}
	if err != nil {
		return nil, err
	}
	filtered := make([]models.EarningEvent, 0, len(events))
	for _, event := range events {
		if event.ID >= gap.ID {
			break
		}
		filtered = append(filtered, event)
	}
	return filtered, nil
}
