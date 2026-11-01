package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"math/big"
	"testing"
	"time"
)

func TestUpdateTaskJobPhaseDoesNotResurrectAbortedJob(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	recordID := uint(41)
	job := models.TaskJob{
		ProjectID:        1,
		UserID:           1,
		PriorityGwei:     models.BigInt{Int: *big.NewInt(10)},
		Model:            "m",
		APIType:          models.TaskAPITypeRaw,
		TaskArgsJSON:     "{}",
		Status:           models.TaskJobStatusFailed,
		Phase:            "failed",
		TaskCallRecordID: &recordID,
		BillingStatus:    models.TaskJobBillingNotBilled,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	eta := time.Now().Add(time.Minute)
	if err := UpdateTaskJobPhase(ctx, db, job.ID, models.TaskJobStatusInProgress, "queued", &eta); err != nil {
		t.Fatal(err)
	}

	var loaded models.TaskJob
	if err := db.First(&loaded, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.Status != models.TaskJobStatusFailed || loaded.Phase != "failed" {
		t.Fatalf("aborted job was resurrected: status=%v phase=%s", loaded.Status, loaded.Phase)
	}
	if loaded.TaskCallRecordID == nil || *loaded.TaskCallRecordID != recordID {
		t.Fatalf("call record changed: %+v", loaded.TaskCallRecordID)
	}
}

func TestUpdateTaskJobPhaseAdvancesSubmittedJob(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	job := models.TaskJob{
		ProjectID:    1,
		UserID:       1,
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Model:        "m",
		APIType:      models.TaskAPITypeRaw,
		TaskArgsJSON: "{}",
		Status:       models.TaskJobStatusSubmitted,
		Phase:        "queued",
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	if err := UpdateTaskJobPhase(ctx, db, job.ID, models.TaskJobStatusInProgress, "running", nil); err != nil {
		t.Fatal(err)
	}

	var loaded models.TaskJob
	if err := db.First(&loaded, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.Status != models.TaskJobStatusInProgress || loaded.Phase != "running" {
		t.Fatalf("expected in_progress/running, got status=%v phase=%s", loaded.Status, loaded.Phase)
	}
}

func TestResetTaskJobToPendingSkipsTerminalAndRecordedJobs(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	recordID := uint(55)
	failed := models.TaskJob{
		ProjectID:        1,
		UserID:           1,
		PriorityGwei:     models.BigInt{Int: *big.NewInt(10)},
		Model:            "m",
		APIType:          models.TaskAPITypeRaw,
		TaskArgsJSON:     "{}",
		Status:           models.TaskJobStatusFailed,
		Phase:            "failed",
		TaskCallRecordID: &recordID,
	}
	if err := db.Create(&failed).Error; err != nil {
		t.Fatal(err)
	}
	if err := ResetTaskJobToPending(ctx, db, failed.ID); err != nil {
		t.Fatal(err)
	}
	var loaded models.TaskJob
	if err := db.First(&loaded, failed.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loaded.Status != models.TaskJobStatusFailed {
		t.Fatalf("terminal job was reset: %+v", loaded)
	}

	inProgress := models.TaskJob{
		ProjectID:        1,
		UserID:           1,
		PriorityGwei:     models.BigInt{Int: *big.NewInt(10)},
		Model:            "m",
		APIType:          models.TaskAPITypeRaw,
		TaskArgsJSON:     "{}",
		Status:           models.TaskJobStatusInProgress,
		Phase:            "running",
		TaskCallRecordID: &recordID,
	}
	if err := db.Create(&inProgress).Error; err != nil {
		t.Fatal(err)
	}
	if err := ResetTaskJobToPending(ctx, db, inProgress.ID); err != nil {
		t.Fatal(err)
	}
	var recorded models.TaskJob
	if err := db.First(&recorded, inProgress.ID).Error; err != nil {
		t.Fatal(err)
	}
	if recorded.Status != models.TaskJobStatusInProgress {
		t.Fatalf("recorded in-progress job was reset: %+v", recorded)
	}
}

func TestFailAndRecordIdempotentWhenStatusResurrected(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	job := models.TaskJob{
		ProjectID:    1,
		UserID:       9,
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		TaskType:     models.TaskTypeLLM,
		Model:        "m",
		APIType:      models.TaskAPITypeChatCompletions,
		TaskArgsJSON: "{}",
		BillingData:  `{"version":1,"priority_gwei":"10","billed_vram":8,"task_fee_wei":"0","llm":{"vram_weight":1,"constant_seconds":1,"seconds_per_input_token":0,"seconds_per_output_token":0,"credits_per_gwei":"1"}}`,
		Status:       models.TaskJobStatusSubmitted,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := FailAndRecordTaskJob(ctx, db, &job, "cancelled by client")
	if err != nil {
		t.Fatal(err)
	}
	if updated.TaskCallRecordID == nil {
		t.Fatal("expected call record")
	}

	if err := db.Model(&models.TaskJob{}).Where("id = ?", updated.ID).Updates(map[string]interface{}{
		"status": models.TaskJobStatusInProgress,
		"phase":  "queued",
	}).Error; err != nil {
		t.Fatal(err)
	}
	resurrected, err := GetTaskJobByID(ctx, db, updated.ID)
	if err != nil {
		t.Fatal(err)
	}

	again, err := FailAndRecordTaskJob(ctx, db, resurrected, "bridge task failed")
	if err != nil {
		t.Fatal(err)
	}
	if again.TaskCallRecordID == nil || *again.TaskCallRecordID != *updated.TaskCallRecordID {
		t.Fatal("fail must remain idempotent after status resurrection")
	}
	var count int64
	if err := db.Model(&models.TaskCallRecord{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("call record count=%d", count)
	}
}

func TestCompleteAndSettleIdempotentWhenAbortedCallRecordExists(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "1000"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "0"}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xabortsettle"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(1000)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-as1",
		APIKeyHash: "h-as1", APIKeyPrefix: "prefixas1",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	c, si, so, vw := 1.0, 0.0, 0.0, 1.0
	job, err := ReserveCreditsAndCreateTaskJob(ctx, db, CreateTaskJobInput{
		Project: &project, APIType: models.TaskAPITypeChatCompletions, Model: "m",
		BilledVram: 8, TaskArgsJSON: "{}", PriorityGwei: big.NewInt(10),
		TaskFeeGwei: big.NewInt(10), VramWeight: &vw, ConstantSeconds: &c,
		SecondsPerInputToken: &si, SecondsPerOutputToken: &so,
		CreditsToLock: big.NewInt(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := FailAndRecordTaskJob(ctx, db, job, "cancelled by client")
	if err != nil {
		t.Fatal(err)
	}

	again, err := CompleteAndSettleTaskJob(ctx, db, failed, `{}`, `{"ok":true}`, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != models.TaskJobStatusFailed {
		t.Fatalf("abort must stay failed, got status=%v", again.Status)
	}
	if again.TaskCallRecordID == nil || *again.TaskCallRecordID != *failed.TaskCallRecordID {
		t.Fatal("settle must not replace abort call record")
	}
	var eventCount int64
	if err := db.Model(&models.CreditEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("eventCount=%d", eventCount)
	}
}
