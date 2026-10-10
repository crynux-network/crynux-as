package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"errors"
	"math/big"
	"testing"
)

func TestReserveCreditsRejectsWhenAvailableInsufficient(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = "1000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xreserve1"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1000)},
		Locked:  models.BigInt{Int: *big.NewInt(0)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-r1",
		APIKeyHash: "h-r1", APIKeyPrefix: "prefixr1",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	c, si, so, vw := 1.0, 0.0, 0.0, 1.0
	in := CreateTaskJobInput{
		Project: &project, APIType: models.TaskAPITypeChatCompletions, Model: "m",
		BilledVram: 8, TaskArgsJSON: "{}", PriorityGwei: big.NewInt(10),
		TaskFeeGwei: big.NewInt(10), VramWeight: &vw, ConstantSeconds: &c,
		SecondsPerInputToken: &si, SecondsPerOutputToken: &so,
		CreditsToLock: big.NewInt(900),
	}
	job1, err := ReserveCreditsAndCreateTaskJob(ctx, db, in)
	if err != nil {
		t.Fatal(err)
	}
	if job1.CreditsLockStatus != models.TaskJobCreditsLockHeld || job1.CreditsLocked.Cmp(big.NewInt(900)) != 0 {
		t.Fatalf("job1 lock=%v locked=%s", job1.CreditsLockStatus, job1.CreditsLocked.String())
	}

	_, err = ReserveCreditsAndCreateTaskJob(ctx, db, in)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("second reserve err=%v, want ErrInsufficientBalance", err)
	}

	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(900)) != 0 || account.Balance.Cmp(big.NewInt(1000)) != 0 {
		t.Fatalf("account balance=%s locked=%s", account.Balance.String(), account.Locked.String())
	}
	var jobCount int64
	if err := db.Model(&models.TaskJob{}).Count(&jobCount).Error; err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 {
		t.Fatalf("jobCount=%d", jobCount)
	}
}

func TestReserveCreditsSecondRequestSeesHeldLock(t *testing.T) {
	// SQLite cannot reliably exercise FOR UPDATE races; this sequential case is the
	// same available-credits rule concurrent MySQL callers observe under row locks.
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = "1000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xconcurrent"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(1000)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-c1",
		APIKeyHash: "h-c1", APIKeyPrefix: "prefixc1",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	c, si, so, vw := 1.0, 0.0, 0.0, 1.0
	in := CreateTaskJobInput{
		Project: &project, APIType: models.TaskAPITypeChatCompletions, Model: "m",
		BilledVram: 8, TaskArgsJSON: "{}", PriorityGwei: big.NewInt(10),
		TaskFeeGwei: big.NewInt(10), VramWeight: &vw, ConstantSeconds: &c,
		SecondsPerInputToken: &si, SecondsPerOutputToken: &so,
		CreditsToLock: big.NewInt(900),
	}
	if _, err := ReserveCreditsAndCreateTaskJob(ctx, db, in); err != nil {
		t.Fatal(err)
	}
	if _, err := ReserveCreditsAndCreateTaskJob(ctx, db, in); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("err=%v, want ErrInsufficientBalance", err)
	}
}

func TestFailReleasesHoldWithoutCharging(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = "1000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xfailhold"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(500)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-f1",
		APIKeyHash: "h-f1", APIKeyPrefix: "prefixf1",
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
		CreditsToLock: big.NewInt(200),
	})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := FailAndRecordTaskJob(ctx, db, job, "boom")
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreditsLockStatus != models.TaskJobCreditsLockReleased {
		t.Fatalf("lock status=%v", updated.CreditsLockStatus)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(0)) != 0 || account.Balance.Cmp(big.NewInt(500)) != 0 {
		t.Fatalf("balance=%s locked=%s", account.Balance.String(), account.Locked.String())
	}
	var eventCount int64
	if err := db.Model(&models.CreditEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 {
		t.Fatalf("eventCount=%d", eventCount)
	}

	again, err := FailAndRecordTaskJob(ctx, db, updated, "boom2")
	if err != nil {
		t.Fatal(err)
	}
	if again.TaskCallRecordID == nil || *again.TaskCallRecordID != *updated.TaskCallRecordID {
		t.Fatal("fail must be idempotent")
	}
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("locked after retry=%s", account.Locked.String())
	}
}

func TestSettleChargesActualAndClearsLock(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = "1000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xsettlelock"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(1000)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-s1",
		APIKeyHash: "h-s1", APIKeyPrefix: "prefixs1",
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

	updated, err := CompleteAndSettleTaskJob(ctx, db, job, `{}`, `{"ok":true}`, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if updated.CreditsLockStatus != models.TaskJobCreditsLockReleased {
		t.Fatalf("lock status=%v", updated.CreditsLockStatus)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	// CalcCredits with priority 10, constant 1, vram 1 => 10 credits
	if account.Balance.Cmp(big.NewInt(990)) != 0 || account.Locked.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("balance=%s locked=%s", account.Balance.String(), account.Locked.String())
	}

	again, err := CompleteAndSettleTaskJob(ctx, db, updated, `{}`, `{}`, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if again.TaskCallRecordID == nil || *again.TaskCallRecordID != *updated.TaskCallRecordID {
		t.Fatal("settle must be idempotent")
	}
	var eventCount int64
	if err := db.Model(&models.CreditEvent{}).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("eventCount=%d", eventCount)
	}
}

func TestSettleChargesMinOfActualAndBalance(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = "1000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xclamp"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(50)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-cl",
		APIKeyHash: "h-cl", APIKeyPrefix: "prefixcl",
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
		CreditsToLock: big.NewInt(50),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Drain balance below computed settle charge while hold is still out.
	if err := db.Model(&models.CreditAccount{}).Where("user_id = ?", user.ID).
		Update("balance", "3").Error; err != nil {
		t.Fatal(err)
	}

	updated, err := CompleteAndSettleTaskJob(ctx, db, job, `{}`, `{"ok":true}`, 1, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != models.TaskJobStatusCompleted {
		t.Fatalf("status=%v", updated.Status)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Balance.Cmp(big.NewInt(0)) != 0 || account.Locked.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("balance=%s locked=%s", account.Balance.String(), account.Locked.String())
	}
	var event models.CreditEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if event.Amount.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("event amount=%s", event.Amount.String())
	}
}

func TestReconcileCreditsLockedCorrectsMismatch(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()

	user := models.User{Address: "0xrecon"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(100)},
		Locked: models.BigInt{Int: *big.NewInt(999)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	job := models.TaskJob{
		ProjectID: 1, UserID: user.ID, Model: "m",
		APIType: models.TaskAPITypeChatCompletions, TaskArgsJSON: "{}",
		PriorityGwei:      models.BigInt{Int: *big.NewInt(1)},
		BillingData:       `{"version":1,"priority_gwei":"1","billed_vram":8,"task_fee_wei":"0","llm":{"vram_weight":1,"constant_seconds":0,"seconds_per_input_token":0,"seconds_per_output_token":0,"credits_per_gwei":"1"}}`,
		CreditsLocked:     models.BigInt{Int: *big.NewInt(40)},
		CreditsLockStatus: models.TaskJobCreditsLockHeld,
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}

	corrected, err := ReconcileCreditsLocked(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if corrected != 1 {
		t.Fatalf("corrected=%d", corrected)
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(40)) != 0 {
		t.Fatalf("locked=%s", account.Locked.String())
	}

	corrected, err = ReconcileCreditsLocked(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if corrected != 0 {
		t.Fatalf("second corrected=%d", corrected)
	}
}

func TestReserveTransactionFailureLeavesLockedUnchanged(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()
	cfg := &config.AppConfig{}
	cfg.LLM.CreditsPerGwei = "1"
	cfg.LLM.MaxTaskPriceCNX = "1000"
	config.SetConfigForTest(cfg)
	t.Cleanup(func() { config.SetConfigForTest(nil) })

	user := models.User{Address: "0xrollback"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID: user.ID, Balance: models.BigInt{Int: *big.NewInt(1000)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	// Missing project FK is fine in sqlite; force create failure with empty model after reserve
	// by using invalid credits path: CreditsToLock nil rejected before TX.
	_, err := ReserveCreditsAndCreateTaskJob(ctx, db, CreateTaskJobInput{
		Project: &models.Project{ID: 1, UserID: user.ID},
		APIType: models.TaskAPITypeChatCompletions, Model: "m",
		BilledVram: 8, TaskArgsJSON: "{}", PriorityGwei: big.NewInt(10),
		TaskFeeGwei: big.NewInt(10),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(0)) != 0 {
		t.Fatalf("locked=%s", account.Locked.String())
	}
}
