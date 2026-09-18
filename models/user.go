package models

import (
	"context"
	"errors"
	"math/big"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint      `json:"id" gorm:"primarykey"`
	CreatedAt time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"not null"`
	Address   string    `json:"address" gorm:"type:string;size:64;not null;uniqueIndex"`
}

// FindUserByAddress returns the user for the given wallet address.
func FindUserByAddress(ctx context.Context, db *gorm.DB, address string) (*User, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var user User
	if err := db.WithContext(dbCtx).Where("address = ?", address).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// EnsureUserWithCreditAccount returns the user for address, creating the user and a
// credit account in one transaction when the user does not exist. When signupBonus
// is greater than zero, the new account balance is set to signupBonus and one
// processed signup-bonus ledger event is written with RefID equal to the user ID.
func EnsureUserWithCreditAccount(ctx context.Context, db *gorm.DB, address string, signupBonus uint64) (*User, error) {
	user, err := FindUserByAddress(ctx, db, address)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var created User
	err = db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		created = User{Address: address}
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
		account := CreditAccount{
			UserID:  created.ID,
			Balance: BigInt{Int: *balance},
		}
		if err := tx.Create(&account).Error; err != nil {
			return err
		}

		if signupBonus == 0 {
			return nil
		}

		event := CreditEvent{
			UserID: created.ID,
			Amount: BigInt{Int: *new(big.Int).SetUint64(signupBonus)},
			Type:   CreditEventTypeSignupBonus,
			RefID:  created.ID,
			Status: CreditEventStatusProcessed,
		}
		return tx.Create(&event).Error
	})
	if err != nil {
		return nil, err
	}

	return &created, nil
}
