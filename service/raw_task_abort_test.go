package service

import (
	"context"
	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"math/big"
	"testing"
)

func TestAbortRawTaskJobPendingSubmit(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "1000"}
	cfg.LLM.MinTaskPriceCNX = map[string]string{config.MaxTaskPriceProviderCrynux: "0"}
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xabort1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1000)},
		Locked:  models.BigInt{Int: *big.NewInt(100)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-abort1",
		APIKeyHash: "h-abort1", APIKeyPrefix: "prefixa1",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	job := models.TaskJob{
		ProjectID:         project.ID,
		UserID:            user.ID,
		PriorityGwei:      models.BigInt{Int: *big.NewInt(10)},
		TaskType:          models.TaskTypeLLM,
		APIType:           models.TaskAPITypeRaw,
		Model:             "m",
		BilledVram:        8,
		TaskArgsJSON:      "{}",
		BillingData:       `{"version":1,"priority_gwei":"10","billed_vram":8,"task_fee_wei":"0","llm":{"vram_weight":1,"constant_seconds":1,"seconds_per_input_token":0,"seconds_per_output_token":0,"credits_per_gwei":"1"}}`,
		Status:            models.TaskJobStatusPendingSubmit,
		CreditsLocked:     models.BigInt{Int: *big.NewInt(100)},
		CreditsLockStatus: models.TaskJobCreditsLockHeld,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	calledBridge := false
	abortBridgeClientTask = func(context.Context, uint) error {
		calledBridge = true
		return nil
	}
	t.Cleanup(func() {
		abortBridgeClientTask = func(ctx context.Context, bridgeClientTaskID uint) error {
			cfg := config.GetConfig()
			client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)
			_, err := client.AbortTask(ctx, bridgeClientTaskID)
			return err
		}
	})

	updated, err := AbortRawTaskJob(ctx, db, project.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calledBridge {
		t.Fatal("pending_submit abort must not call Bridge")
	}
	if updated.Status != models.TaskJobStatusFailed || updated.ErrorMessage == nil || *updated.ErrorMessage != RawTaskAbortErrorMessage {
		t.Fatalf("job=%+v", updated)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("locked=%s", account.Locked.String())
	}

	again, err := AbortRawTaskJob(ctx, db, project.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != models.TaskJobStatusFailed {
		t.Fatalf("idempotent abort failed: %+v", again)
	}
}

func TestAbortRawTaskJobBridgeSuccess(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	user := models.User{Address: "0xabort2"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1000)},
		Locked:  models.BigInt{Int: *big.NewInt(50)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p2", EndpointToken: "e-abort2",
		APIKeyHash: "h-abort2", APIKeyPrefix: "prefixa2",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	bridgeID := uint(77)
	job := models.TaskJob{
		ProjectID:          project.ID,
		UserID:             user.ID,
		PriorityGwei:       models.BigInt{Int: *big.NewInt(10)},
		TaskType:           models.TaskTypeLLM,
		APIType:            models.TaskAPITypeRaw,
		Model:              "m",
		BilledVram:         8,
		TaskArgsJSON:       "{}",
		BillingData:        `{"version":1,"priority_gwei":"10","billed_vram":8,"task_fee_wei":"0","llm":{"vram_weight":1,"constant_seconds":1,"seconds_per_input_token":0,"seconds_per_output_token":0,"credits_per_gwei":"1"}}`,
		BridgeClientTaskID: &bridgeID,
		Status:             models.TaskJobStatusSubmitted,
		CreditsLocked:      models.BigInt{Int: *big.NewInt(50)},
		CreditsLockStatus:  models.TaskJobCreditsLockHeld,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	var abortedID uint
	abortBridgeClientTask = func(_ context.Context, id uint) error {
		abortedID = id
		return nil
	}
	t.Cleanup(func() {
		abortBridgeClientTask = func(ctx context.Context, bridgeClientTaskID uint) error {
			cfg := config.GetConfig()
			client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)
			_, err := client.AbortTask(ctx, bridgeClientTaskID)
			return err
		}
	})

	updated, err := AbortRawTaskJob(ctx, db, project.ID, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if abortedID != bridgeID {
		t.Fatalf("aborted bridge id=%d", abortedID)
	}
	if updated.Status != models.TaskJobStatusFailed {
		t.Fatalf("status=%v", updated.Status)
	}
}

func TestAbortRawTaskJobBridgeNotCancellable(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	user := models.User{Address: "0xabort3"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1000)},
		Locked:  models.BigInt{Int: *big.NewInt(50)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p3", EndpointToken: "e-abort3",
		APIKeyHash: "h-abort3", APIKeyPrefix: "prefixa3",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	bridgeID := uint(88)
	job := models.TaskJob{
		ProjectID:          project.ID,
		UserID:             user.ID,
		PriorityGwei:       models.BigInt{Int: *big.NewInt(10)},
		TaskType:           models.TaskTypeLLM,
		APIType:            models.TaskAPITypeRaw,
		Model:              "m",
		BilledVram:         8,
		TaskArgsJSON:       "{}",
		BillingData:        `{"version":1,"priority_gwei":"10","billed_vram":8,"task_fee_wei":"0","llm":{"vram_weight":1,"constant_seconds":1,"seconds_per_input_token":0,"seconds_per_output_token":0,"credits_per_gwei":"1"}}`,
		BridgeClientTaskID: &bridgeID,
		Status:             models.TaskJobStatusInProgress,
		CreditsLocked:      models.BigInt{Int: *big.NewInt(50)},
		CreditsLockStatus:  models.TaskJobCreditsLockHeld,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	abortBridgeClientTask = func(context.Context, uint) error {
		return bridge.ErrAbortNotCancellable
	}
	t.Cleanup(func() {
		abortBridgeClientTask = func(ctx context.Context, bridgeClientTaskID uint) error {
			cfg := config.GetConfig()
			client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)
			_, err := client.AbortTask(ctx, bridgeClientTaskID)
			return err
		}
	})

	_, err := AbortRawTaskJob(ctx, db, project.ID, job.ID)
	if !errors.Is(err, ErrRawTaskAbortNotCancellable) {
		t.Fatalf("err=%v", err)
	}
	var reloaded models.TaskJob
	if err := db.First(&reloaded, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != models.TaskJobStatusInProgress || reloaded.CreditsLockStatus != models.TaskJobCreditsLockHeld {
		t.Fatalf("job changed on failed abort: %+v", reloaded)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(50)) != 0 {
		t.Fatalf("locked=%s", account.Locked.String())
	}
}

func TestAbortRawTaskJobCompletedConflict(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	project := models.Project{
		UserID: 1, Name: "p4", EndpointToken: "e-abort4",
		APIKeyHash: "h-abort4", APIKeyPrefix: "prefixa4",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	job := models.TaskJob{
		ProjectID:    project.ID,
		UserID:       1,
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		TaskType:     models.TaskTypeLLM,
		APIType:      models.TaskAPITypeRaw,
		Model:        "m",
		TaskArgsJSON: "{}",
		Status:       models.TaskJobStatusCompleted,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	_, err := AbortRawTaskJob(ctx, db, project.ID, job.ID)
	if !errors.Is(err, ErrRawTaskAbortAlreadyComplete) {
		t.Fatalf("err=%v", err)
	}
}

func TestAbortRawTaskJobsBatchIndependence(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	user := models.User{Address: "0xabort5"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(1000)}, Locked: models.BigInt{Int: *big.NewInt(10)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p5", EndpointToken: "e-abort5",
		APIKeyHash: "h-abort5", APIKeyPrefix: "prefixa5",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}
	pending := models.TaskJob{
		ProjectID: project.ID, UserID: user.ID, PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		TaskType: models.TaskTypeLLM, APIType: models.TaskAPITypeRaw, Model: "m", BilledVram: 8,
		TaskArgsJSON: "{}", BillingData: `{"version":1,"priority_gwei":"10","billed_vram":8,"task_fee_wei":"0","llm":{"vram_weight":1,"constant_seconds":1,"seconds_per_input_token":0,"seconds_per_output_token":0,"credits_per_gwei":"1"}}`,
		Status: models.TaskJobStatusPendingSubmit, CreditsLocked: models.BigInt{Int: *big.NewInt(10)},
		CreditsLockStatus: models.TaskJobCreditsLockHeld,
	}
	completed := models.TaskJob{
		ProjectID: project.ID, UserID: user.ID, PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		TaskType: models.TaskTypeLLM, APIType: models.TaskAPITypeRaw, Model: "m",
		TaskArgsJSON: "{}", Status: models.TaskJobStatusCompleted,
	}
	if err := db.Create(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&completed).Error; err != nil {
		t.Fatal(err)
	}
	results, err := AbortRawTaskJobs(ctx, db, project.ID, []uint{pending.ID, completed.ID})
	if err != nil {
		t.Fatal(err)
	}
	if results[0].Error != "" || results[0].Job == nil || results[0].Job.Status != models.TaskJobStatusFailed {
		t.Fatalf("first=%+v", results[0])
	}
	if results[1].Error != "not_cancellable" {
		t.Fatalf("second=%+v", results[1])
	}
}
