package service

import (
	"context"
	"crynux_as/models"
	"math/big"
	"testing"
)

func TestFailPriorProcessProviderJobsFailsAdmittingAndLeavesQueued(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()

	user := models.User{Address: "0xprovider-recover"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1000)},
		Locked:  models.BigInt{Int: *big.NewInt(160)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-pr",
		APIKeyHash: "h-pr", APIKeyPrefix: "prefixpr",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	billing := `{"version":1,"priority_gwei":"10","billed_vram":0,"task_fee_wei":"0","locked_credits":"40","provider":{"service_provider":"openai","model_name":"m","max_task_price_cnx":"1","input_price_per_million_tokens_cnx":"1","output_price_per_million_tokens_cnx":"1","estimated_input_tokens":1,"estimated_output_tokens":1,"estimated_credits":"40","effective_priority_gwei":"10"}}`
	makeJob := func(status models.TaskJobStatus, phase string, locked int64) models.TaskJob {
		return models.TaskJob{
			ProjectID:         project.ID,
			UserID:            user.ID,
			PriorityGwei:      models.BigInt{Int: *big.NewInt(10)},
			SortablePriority:  models.FormatSortablePriority(big.NewInt(10)),
			ExecutionSource:   models.ExecutionSourceOpenAI,
			TaskType:          models.TaskTypeLLM,
			APIType:           models.TaskAPITypeChatCompletions,
			Model:             "m",
			TaskArgsJSON:      "{}",
			BillingData:       billing,
			QueueTimeout:      60,
			Status:            status,
			Phase:             phase,
			CreditsLocked:     models.BigInt{Int: *big.NewInt(locked)},
			CreditsLockStatus: models.TaskJobCreditsLockHeld,
			Stream:            phase == "admitting",
		}
	}

	admitting := makeJob(models.TaskJobStatusPendingSubmit, "admitting", 40)
	queued := makeJob(models.TaskJobStatusPendingSubmit, "queued", 40)
	submitted := makeJob(models.TaskJobStatusSubmitted, "submitted", 40)
	inProgress := makeJob(models.TaskJobStatusInProgress, "in_progress", 40)
	for _, job := range []*models.TaskJob{&admitting, &queued, &submitted, &inProgress} {
		if err := db.Create(job).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := FailPriorProcessProviderJobs(ctx, db); err != nil {
		t.Fatal(err)
	}

	assertFailedReleased := func(id uint) {
		t.Helper()
		var job models.TaskJob
		if err := db.First(&job, id).Error; err != nil {
			t.Fatal(err)
		}
		if job.Status != models.TaskJobStatusFailed || job.CreditsLockStatus != models.TaskJobCreditsLockReleased {
			t.Fatalf("job %d status=%v lock=%v phase=%s", id, job.Status, job.CreditsLockStatus, job.Phase)
		}
	}
	assertFailedReleased(admitting.ID)
	assertFailedReleased(submitted.ID)
	assertFailedReleased(inProgress.ID)

	var queuedJob models.TaskJob
	if err := db.First(&queuedJob, queued.ID).Error; err != nil {
		t.Fatal(err)
	}
	if queuedJob.Status != models.TaskJobStatusPendingSubmit || queuedJob.Phase != "queued" || queuedJob.CreditsLockStatus != models.TaskJobCreditsLockHeld {
		t.Fatalf("queued job=%+v", queuedJob)
	}

	var account models.CreditAccount
	if err := db.Where("user_id = ?", user.ID).First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Locked.Cmp(big.NewInt(40)) != 0 {
		t.Fatalf("account locked=%s, want 40", account.Locked.String())
	}
}

func TestReserveCreditsAndCreateProviderTaskJobStreamingStartsAdmitting(t *testing.T) {
	db := setupUsageStatsTestDB(t)
	ctx := context.Background()

	user := models.User{Address: "0xprovider-admit"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&models.CreditAccount{
		UserID:  user.ID,
		Balance: models.BigInt{Int: *big.NewInt(1000)},
	}).Error; err != nil {
		t.Fatal(err)
	}
	project := models.Project{
		UserID: user.ID, Name: "p", EndpointToken: "e-pa",
		APIKeyHash: "h-pa", APIKeyPrefix: "prefixpa",
		PriorityGwei: models.BigInt{Int: *big.NewInt(10)},
		Status:       models.ProjectStatusActive,
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatal(err)
	}

	job, err := ReserveCreditsAndCreateProviderTaskJob(ctx, db, CreateProviderTaskJobInput{
		Project:       &project,
		APIType:       models.TaskAPITypeChatCompletions,
		Route:         ProviderModelRoute{ModelName: "m", ServiceProvider: models.ServiceProviderOpenAI},
		Stream:        true,
		RequestBody:   []byte(`{}`),
		PriorityGwei:  big.NewInt(10),
		CreditsToLock: big.NewInt(50),
		QueueTimeout:  60,
		BillingSnapshot: models.ProviderTaskBillingData{
			ServiceProvider:       string(models.ServiceProviderOpenAI),
			ModelName:             "m",
			MaxTaskPriceCNX:       "1",
			EstimatedOutputTokens: 1,
			LockedCredits:         "50",
			EffectivePriorityGwei: "10",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if job.Phase != "admitting" || job.Status != models.TaskJobStatusPendingSubmit {
		t.Fatalf("job phase=%s status=%v", job.Phase, job.Status)
	}
}
