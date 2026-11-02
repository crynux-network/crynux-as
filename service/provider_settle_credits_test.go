package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/provider"
	"math/big"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupProviderSettleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.CreditAccount{},
		&models.CreditEvent{},
		&models.Project{},
		&models.TaskJob{},
		&models.TaskCallRecord{},
		&models.UsageStatsPendingCallRecord{},
		&models.SharedAPIKey{},
		&models.EarningAccount{},
		&models.EarningEvent{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestSettleProviderTaskJobPreservesOtherLocksAndFullEarning(t *testing.T) {
	db := setupProviderSettleTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"openai": "1000"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	consumer := models.User{Address: "0xconsumer"}
	owner := models.User{Address: "0xowner"}
	if err := db.Create(&consumer).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: consumer.ID, Balance: models.BigInt{Int: *big.NewInt(200)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: consumer.ID, Name: "p", EndpointToken: "e-ps",
		APIKeyHash: "h-ps", APIKeyPrefix: "prefixps",
		PriorityGwei: models.BigInt{Int: *big.NewInt(1)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	fp := "fp-ps"
	key := models.SharedAPIKey{
		PublicID: "key_ps", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		UserID: owner.ID, ServiceProvider: models.ServiceProviderOpenAI,
		MaskedSuffix: "abcd", OwnerEnabled: true, Fingerprint: &fp,
		KeyCiphertext: []byte{1}, KeyNonce: []byte{2},
	}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}

	snapshot := models.ProviderTaskBillingData{
		ServiceProvider:       string(models.ServiceProviderOpenAI),
		ModelName:             "gpt-6-astra",
		UsageWeights:          map[string]string{"input_tokens": "1", "output_tokens": "1"},
		EffectivePriorityGwei: "1",
		CreditsPerGwei:        "1",
		MaxTaskPriceCNX:       "1000",
		EstimatedInputTokens:  100,
		EstimatedOutputTokens: 1,
		LockedCredits:         "100",
	}
	jobA, err := ReserveCreditsAndCreateProviderTaskJob(ctx, db, CreateProviderTaskJobInput{
		Project: &project, APIType: models.TaskAPITypeChatCompletions,
		Route:       ProviderModelRoute{ServiceProvider: models.ServiceProviderOpenAI, ModelName: "gpt-6-astra"},
		RequestBody: []byte(`{}`), PriorityGwei: big.NewInt(1), CreditsToLock: big.NewInt(100),
		QueueTimeout: 60, BillingSnapshot: snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReserveCreditsAndCreateProviderTaskJob(ctx, db, CreateProviderTaskJobInput{
		Project: &project, APIType: models.TaskAPITypeChatCompletions,
		Route:       ProviderModelRoute{ServiceProvider: models.ServiceProviderOpenAI, ModelName: "gpt-6-astra"},
		RequestBody: []byte(`{}`), PriorityGwei: big.NewInt(1), CreditsToLock: big.NewInt(100),
		QueueTimeout: 60, BillingSnapshot: snapshot,
	}); err != nil {
		t.Fatal(err)
	}

	// Actual usage costs 150 credits at priority 1; only 100 available after protecting B's lock.
	if err := SettleProviderTaskJob(ctx, db, jobA, key.ID, owner.ID, owner.Address, `{}`, `{}`, provider.NormalizedUsage{
		InputTokens: 150, OutputTokens: 0, TotalTokens: 150,
	}); err != nil {
		t.Fatal(err)
	}

	var account models.CreditAccount
	if err := db.Where("user_id = ?", consumer.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Balance.Cmp(big.NewInt(100)) != 0 || account.Locked.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("balance=%s locked=%s", account.Balance.String(), account.Locked.String())
	}
	var charge models.CreditEvent
	if err := db.First(&charge).Error; err != nil {
		t.Fatal(err)
	}
	if charge.Amount.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("charged=%s", charge.Amount.String())
	}

	var earning models.EarningAccount
	if err := db.Where("user_id = ?", owner.ID).First(&earning).Error; err != nil {
		t.Fatal(err)
	}
	wantEarning := ProviderEarningWei(big.NewRat(150, 1))
	if earning.Available.Cmp(wantEarning) != 0 {
		t.Fatalf("earning available=%s want=%s", earning.Available.String(), wantEarning.String())
	}
	var earningEvent models.EarningEvent
	if err := db.First(&earningEvent).Error; err != nil {
		t.Fatal(err)
	}
	if earningEvent.Amount.Cmp(wantEarning) != 0 {
		t.Fatalf("earning event=%s", earningEvent.Amount.String())
	}
}

func TestSettleProviderTaskJobZeroConsumerChargeStillPaysOwner(t *testing.T) {
	db := setupProviderSettleTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{"openai": "1000"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{
		config.MaxTaskPriceProviderCrynux: "0",
		config.MinTaskPriceProviders:      "0",
	}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	consumer := models.User{Address: "0xconsumer2"}
	owner := models.User{Address: "0xowner2"}
	if err := db.Create(&consumer).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: consumer.ID, Balance: models.BigInt{Int: *big.NewInt(200)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: consumer.ID, Name: "p", EndpointToken: "e-ps2",
		APIKeyHash: "h-ps2", APIKeyPrefix: "prefixps2",
		PriorityGwei: models.BigInt{Int: *big.NewInt(1)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	fp := "fp-ps2"
	key := models.SharedAPIKey{
		PublicID: "key_ps2", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		UserID: owner.ID, ServiceProvider: models.ServiceProviderOpenAI,
		MaskedSuffix: "abcd", OwnerEnabled: true, Fingerprint: &fp,
		KeyCiphertext: []byte{1}, KeyNonce: []byte{2},
	}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := models.ProviderTaskBillingData{
		ServiceProvider: string(models.ServiceProviderOpenAI), ModelName: "gpt-6-astra",
		UsageWeights:          map[string]string{"input_tokens": "1", "output_tokens": "1"},
		EffectivePriorityGwei: "1", CreditsPerGwei: "1", MaxTaskPriceCNX: "1000",
		LockedCredits: "100",
	}
	jobA, err := ReserveCreditsAndCreateProviderTaskJob(ctx, db, CreateProviderTaskJobInput{
		Project: &project, APIType: models.TaskAPITypeChatCompletions,
		Route:       ProviderModelRoute{ServiceProvider: models.ServiceProviderOpenAI, ModelName: "gpt-6-astra"},
		RequestBody: []byte(`{}`), PriorityGwei: big.NewInt(1), CreditsToLock: big.NewInt(100),
		QueueTimeout: 60, BillingSnapshot: snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReserveCreditsAndCreateProviderTaskJob(ctx, db, CreateProviderTaskJobInput{
		Project: &project, APIType: models.TaskAPITypeChatCompletions,
		Route:       ProviderModelRoute{ServiceProvider: models.ServiceProviderOpenAI, ModelName: "gpt-6-astra"},
		RequestBody: []byte(`{}`), PriorityGwei: big.NewInt(1), CreditsToLock: big.NewInt(100),
		QueueTimeout: 60, BillingSnapshot: snapshot,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.CreditAccount{}).Where("user_id = ?", consumer.ID).
		Update("balance", "100").Error; err != nil {
		t.Fatal(err)
	}

	if err := SettleProviderTaskJob(ctx, db, jobA, key.ID, owner.ID, owner.Address, `{}`, `{}`, provider.NormalizedUsage{
		InputTokens: 80, OutputTokens: 0, TotalTokens: 80,
	}); err != nil {
		t.Fatal(err)
	}
	var eventCount int64
	if err := db.Model(&models.CreditEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("consumer charge events=%d", eventCount)
	}
	var earning models.EarningAccount
	if err := db.Where("user_id = ?", owner.ID).First(&earning).Error; err != nil {
		t.Fatal(err)
	}
	want := ProviderEarningWei(big.NewRat(80, 1))
	if earning.Available.Cmp(want) != 0 {
		t.Fatalf("earning=%s want=%s", earning.Available.String(), want.String())
	}
}
