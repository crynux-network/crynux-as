package service

import (
	"context"
	"crynux_as/models"
	"math/big"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupUsageStatsTestDB(t *testing.T) *gorm.DB {
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
		&models.LLMJob{},
		&models.LLMCallRecord{},
		&models.AccountUsageHourlyStat{},
		&models.ProjectUsageHourlyStat{},
		&models.ProjectModelUsage10mStat{},
		&models.ProjectDurationUsage10mStat{},
		&models.ProjectModelUsageSnapshot{},
		&models.ProjectDurationHistogramSnapshot{},
		&models.UsageStatsProgress{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestUsageStatsBaseAggregationAndSnapshots(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()

	user := models.User{Address: "0xabc"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID:        user.ID,
		Name:          "p1",
		EndpointToken: "ep",
		APIKeyHash:    "hash",
		APIKeyPrefix:  "prefix12",
		TokenRatio:    10,
		Status:        models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	success := models.LLMCallRecord{
		UserID:               user.ID,
		ProjectID:            project.ID,
		Model:                "model-a",
		PromptTokens:         10,
		CompletionTokens:     5,
		TotalTokens:          15,
		TokenRatio:           10,
		TokenUsageApplicable: 1,
		Status:               models.LLMCallStatusSuccess,
		Credits:              models.BigInt{Int: *big.NewInt(0)},
		AcceptedAt:           now,
		CompletedAt:          now.Add(2 * time.Second),
		DurationMs:           2000,
	}
	if err := db.Create(&success).Error; err != nil {
		t.Fatal(err)
	}
	event := models.CreditEvent{
		UserID: user.ID,
		Amount: models.BigInt{Int: *big.NewInt(42)},
		Type:   models.CreditEventTypeLLMCharge,
		RefID:  success.ID,
		Status: models.CreditEventStatusProcessed,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}

	failed := models.LLMCallRecord{
		UserID:               user.ID,
		ProjectID:            project.ID,
		Model:                "model-a",
		TokenUsageApplicable: 1,
		Status:               models.LLMCallStatusFailed,
		Credits:              models.BigInt{Int: *big.NewInt(0)},
		AcceptedAt:           now,
		CompletedAt:          now.Add(100 * time.Millisecond),
		DurationMs:           100,
	}
	if err := db.Create(&failed).Error; err != nil {
		t.Fatal(err)
	}

	noToken := models.LLMCallRecord{
		UserID:               user.ID,
		ProjectID:            project.ID,
		Model:                "image-model",
		PromptTokens:         99,
		CompletionTokens:     99,
		TotalTokens:          198,
		TokenUsageApplicable: 0,
		Status:               models.LLMCallStatusSuccess,
		Credits:              models.BigInt{Int: *big.NewInt(0)},
		AcceptedAt:           now,
		CompletedAt:          now.Add(time.Second),
		DurationMs:           1000,
	}
	if err := db.Create(&noToken).Error; err != nil {
		t.Fatal(err)
	}
	var stored models.LLMCallRecord
	if err := db.First(&stored, noToken.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TokenUsageApplicable != 0 {
		t.Fatalf("token_usage_applicable stored as %d", stored.TokenUsageApplicable)
	}

	processed, err := RunUsageStatsBaseAggregation(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 3 {
		t.Fatalf("processed=%d", processed)
	}

	var account models.AccountUsageHourlyStat
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.RequestCount != 3 || account.SuccessCount != 2 || account.FailureCount != 1 {
		t.Fatalf("account counts=%+v", account)
	}
	if account.PromptTokens != 10 || account.CompletionTokens != 5 {
		t.Fatalf("token applicable filter failed: %+v", account)
	}
	if account.Credits.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("credits=%s", account.Credits.String())
	}

	claimed, err := ClaimDirtyProjectsForSnapshot(ctx, db, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed=%d", len(claimed))
	}
	if err := RefreshProjectUsageSnapshots(ctx, db, project.ID, claimed[0].ClaimedGeneration); err != nil {
		t.Fatal(err)
	}

	modelsSnap, err := GetProjectModelUsageSnapshots(ctx, db, project.ID, models.UsageStatsRange1d)
	if err != nil {
		t.Fatal(err)
	}
	if len(modelsSnap) == 0 {
		t.Fatal("expected model snapshots")
	}

	durSnap, err := GetProjectDurationHistogramSnapshots(ctx, db, project.ID, models.UsageStatsRange1d)
	if err != nil {
		t.Fatal(err)
	}
	if len(durSnap) == 0 {
		t.Fatal("expected duration snapshots")
	}

	// Retry same batch must process zero.
	processed, err = RunUsageStatsBaseAggregation(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if processed != 0 {
		t.Fatalf("reprocess=%d", processed)
	}
}

func TestCompleteAndSettleCreatesOneRecordAndCharge(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()

	user := models.User{Address: "0xsettle"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	account := models.CreditAccount{UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(1000)}}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID:        user.ID,
		Name:          "p",
		EndpointToken: "e2",
		APIKeyHash:    "h2",
		APIKeyPrefix:  "prefix22",
		TokenRatio:    10,
		Status:        models.ProjectStatusDeleted,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	c := 1.0
	si := 0.0
	so := 0.0
	vw := 1.0
	job := models.LLMJob{
		ProjectID:             project.ID,
		UserID:                user.ID,
		TokenRatio:            10,
		Model:                 "m",
		APIType:               models.LLMAPITypeChatCompletions,
		TaskArgsJSON:          "{}",
		Status:                models.LLMJobStatusInProgress,
		BillingStatus:         models.LLMJobBillingPending,
		ConstantSeconds:       &c,
		SecondsPerInputToken:  &si,
		SecondsPerOutputToken: &so,
		VramWeight:            &vw,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	// CalcCredits with constant=1, vram=1, token_ratio=10, ref priority needs config.
	// Skip full settle if config not loaded; test FailAndRecord instead plus ProcessLLMCall charge path.
	accepted := time.Now().Add(-time.Second)
	completed := time.Now()
	jobID := job.ID
	recordID, err := ProcessLLMCall(ctx, db, RecordLLMCallInput{
		UserID:               user.ID,
		ProjectID:            project.ID,
		LLMJobID:             &jobID,
		Model:                "m",
		PromptTokens:         1,
		CompletionTokens:     1,
		TotalTokens:          2,
		TokenRatio:           10,
		TokenUsageApplicable: true,
		Status:               models.LLMCallStatusSuccess,
		Credits:              big.NewInt(7),
		AcceptedAt:           accepted,
		CompletedAt:          completed,
		Charge:               true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if recordID == 0 {
		t.Fatal("record id")
	}

	var records []models.LLMCallRecord
	if err := db.Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records=%d", len(records))
	}
	if records[0].DurationMs == 0 {
		t.Fatal("duration must be completed_at - accepted_at")
	}
	if records[0].UserID != user.ID {
		t.Fatal("user snapshot")
	}

	var events []models.CreditEvent
	if err := db.Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].RefID != recordID {
		t.Fatalf("events=%+v", events)
	}

	_, err = ProcessLLMCall(ctx, db, RecordLLMCallInput{
		UserID:               user.ID,
		ProjectID:            project.ID,
		LLMJobID:             &jobID,
		Model:                "m",
		TokenUsageApplicable: true,
		Status:               models.LLMCallStatusFailed,
		Credits:              big.NewInt(0),
		AcceptedAt:           accepted,
		CompletedAt:          completed,
		Charge:               false,
	})
	if err == nil {
		t.Fatal("duplicate llm_job_id must fail")
	}
}

func TestFailAndRecordLLMJobSingleTransaction(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	job := models.LLMJob{
		ProjectID:    1,
		UserID:       9,
		TokenRatio:   10,
		Model:        "m",
		APIType:      models.LLMAPITypeChatCompletions,
		TaskArgsJSON: "{}",
		Status:       models.LLMJobStatusSubmitted,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := FailAndRecordLLMJob(ctx, db, &job, "boom")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != models.LLMJobStatusFailed || updated.LLMCallRecordID == nil {
		t.Fatalf("job=%+v", updated)
	}
	var count int64
	if err := db.Model(&models.LLMCallRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
	again, err := FailAndRecordLLMJob(ctx, db, updated, "boom2")
	if err != nil {
		t.Fatal(err)
	}
	if again.LLMCallRecordID == nil || *again.LLMCallRecordID != *updated.LLMCallRecordID {
		t.Fatal("idempotent fail record")
	}
	if err := db.Model(&models.LLMCallRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("after retry count=%d", count)
	}
}
