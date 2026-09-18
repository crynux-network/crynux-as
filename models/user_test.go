package models

import (
	"context"
	"math/big"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&User{}, &CreditAccount{}, &CreditEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestEnsureUserWithCreditAccountGrantsSignupBonus(t *testing.T) {
	db := setupUserTestDB(t)
	ctx := context.Background()
	address := "0x1111111111111111111111111111111111111111"

	user, err := EnsureUserWithCreditAccount(ctx, db, address, 1000)
	if err != nil {
		t.Fatal(err)
	}

	var account CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Balance.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance=%s want 1000", account.Balance.String())
	}

	var event CreditEvent
	if err := db.Where("user_id = ? AND type = ?", user.ID, CreditEventTypeSignupBonus).First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.RefID != user.ID {
		t.Fatalf("ref_id=%d want %d", event.RefID, user.ID)
	}
	if event.Amount.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("event amount=%s want 1000", event.Amount.String())
	}
	if event.Status != CreditEventStatusProcessed {
		t.Fatalf("event status=%d want processed", event.Status)
	}
}

func TestEnsureUserWithCreditAccountZeroBonusSkipsEvent(t *testing.T) {
	db := setupUserTestDB(t)
	ctx := context.Background()
	address := "0x2222222222222222222222222222222222222222"

	user, err := EnsureUserWithCreditAccount(ctx, db, address, 0)
	if err != nil {
		t.Fatal(err)
	}

	var account CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Balance.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("balance=%s want 0", account.Balance.String())
	}

	var count int64
	if err := db.Model(&CreditEvent{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("event count=%d want 0", count)
	}
}

func TestEnsureUserWithCreditAccountExistingUserUnchanged(t *testing.T) {
	db := setupUserTestDB(t)
	ctx := context.Background()
	address := "0x3333333333333333333333333333333333333333"

	first, err := EnsureUserWithCreditAccount(ctx, db, address, 1000)
	if err != nil {
		t.Fatal(err)
	}

	second, err := EnsureUserWithCreditAccount(ctx, db, address, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("user id changed: first=%d second=%d", first.ID, second.ID)
	}

	var account CreditAccount
	if err := db.Where("user_id = ?", first.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Balance.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("balance=%s want 1000", account.Balance.String())
	}

	var count int64
	if err := db.Model(&CreditEvent{}).Where("user_id = ? AND type = ?", first.ID, CreditEventTypeSignupBonus).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("signup bonus event count=%d want 1", count)
	}
}
