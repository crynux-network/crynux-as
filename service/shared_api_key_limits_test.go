package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupSharedAPIKeyLimitsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.SharedAPIKey{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func createTestSharedAPIKey(t *testing.T, db *gorm.DB, userID uint, publicID string) models.SharedAPIKey {
	t.Helper()
	fp := "fp-" + publicID
	row := models.SharedAPIKey{
		PublicID:        publicID,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
		UserID:          userID,
		ServiceProvider: models.ServiceProviderOpenAI,
		MaskedSuffix:    "abcd",
		OwnerEnabled:    true,
		Fingerprint:     &fp,
		KeyCiphertext:   []byte{1},
		KeyNonce:        []byte{2},
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create key: %v", err)
	}
	return row
}

func TestListEligibleSharedAPIKeysExcludesExhaustedLimits(t *testing.T) {
	db := setupSharedAPIKeyLimitsTestDB(t)
	ctx := context.Background()
	user := models.User{Address: "0xlimits1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}

	unlimited := createTestSharedAPIKey(t, db, user.ID, "key_unlimited")
	inputExhausted := createTestSharedAPIKey(t, db, user.ID, "key_input")
	outputExhausted := createTestSharedAPIKey(t, db, user.ID, "key_output")
	underLimit := createTestSharedAPIKey(t, db, user.ID, "key_under")

	maxIn := uint64(100)
	maxOut := uint64(50)
	if err := db.Model(&inputExhausted).Updates(map[string]interface{}{
		"used_input_tokens": uint64(100),
		"max_input_tokens":  maxIn,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&outputExhausted).Updates(map[string]interface{}{
		"used_output_tokens": uint64(50),
		"max_output_tokens":  maxOut,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&underLimit).Updates(map[string]interface{}{
		"used_input_tokens":  uint64(10),
		"used_output_tokens": uint64(5),
		"max_input_tokens":   maxIn,
		"max_output_tokens":  maxOut,
	}).Error; err != nil {
		t.Fatal(err)
	}

	keys, err := ListEligibleSharedAPIKeys(ctx, db, models.ServiceProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, key := range keys {
		got[key.PublicID] = true
	}
	if !got[unlimited.PublicID] || !got[underLimit.PublicID] {
		t.Fatalf("expected unlimited and under-limit keys, got %#v", got)
	}
	if got[inputExhausted.PublicID] || got[outputExhausted.PublicID] {
		t.Fatalf("exhausted keys must be excluded, got %#v", got)
	}
}

func TestUpdateSharedAPIKeyLimitsAndReset(t *testing.T) {
	db := setupSharedAPIKeyLimitsTestDB(t)
	ctx := context.Background()
	user := models.User{Address: "0xlimits2"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	key := createTestSharedAPIKey(t, db, user.ID, "key_limits")

	zero := uint64(0)
	if _, err := UpdateSharedAPIKeyLimits(ctx, db, user.ID, key.PublicID, &zero, nil); !errors.Is(err, ErrSharedAPIKeyInvalidLimit) {
		t.Fatalf("expected invalid limit, got %v", err)
	}

	maxIn := uint64(1000)
	updated, err := UpdateSharedAPIKeyLimits(ctx, db, user.ID, key.PublicID, &maxIn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if updated.MaxInputTokens == nil || *updated.MaxInputTokens != 1000 || updated.MaxOutputTokens != nil {
		t.Fatalf("unexpected limits: %+v %+v", updated.MaxInputTokens, updated.MaxOutputTokens)
	}

	if err := db.Model(&key).Updates(map[string]interface{}{
		"used_input_tokens":  uint64(900),
		"used_output_tokens": uint64(40),
	}).Error; err != nil {
		t.Fatal(err)
	}

	lower := uint64(100)
	updated, err = UpdateSharedAPIKeyLimits(ctx, db, user.ID, key.PublicID, &lower, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ListEligibleSharedAPIKeys(ctx, db, models.ServiceProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("key under lowered max must be excluded, got %d", len(keys))
	}

	reset, err := ResetSharedAPIKeyUsage(ctx, db, user.ID, key.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if reset.UsedInputTokens != 0 || reset.UsedOutputTokens != 0 {
		t.Fatalf("reset must zero counters: %+v", reset)
	}
	if reset.MaxInputTokens == nil || *reset.MaxInputTokens != 100 {
		t.Fatalf("reset must keep max: %+v", reset.MaxInputTokens)
	}
	keys, err = ListEligibleSharedAPIKeys(ctx, db, models.ServiceProviderOpenAI)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].PublicID != key.PublicID {
		t.Fatalf("reset must restore eligibility, got %#v", keys)
	}

	cleared, err := UpdateSharedAPIKeyLimits(ctx, db, user.ID, key.PublicID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.MaxInputTokens != nil || cleared.MaxOutputTokens != nil {
		t.Fatalf("expected unlimited after clear: %+v %+v", cleared.MaxInputTokens, cleared.MaxOutputTokens)
	}
}

func TestAddSharedAPIKeyUsageTx(t *testing.T) {
	db := setupSharedAPIKeyLimitsTestDB(t)
	user := models.User{Address: "0xlimits3"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	key := createTestSharedAPIKey(t, db, user.ID, "key_usage")

	err := db.Transaction(func(tx *gorm.DB) error {
		return AddSharedAPIKeyUsageTx(tx, key.ID, 12, 7)
	})
	if err != nil {
		t.Fatal(err)
	}
	var stored models.SharedAPIKey
	if err := db.First(&stored, key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.UsedInputTokens != 12 || stored.UsedOutputTokens != 7 {
		t.Fatalf("unexpected usage: %+v", stored)
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		return AddSharedAPIKeyUsageTx(tx, key.ID, 3, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.First(&stored, key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.UsedInputTokens != 15 || stored.UsedOutputTokens != 8 {
		t.Fatalf("unexpected cumulative usage: %+v", stored)
	}
}
