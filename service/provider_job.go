package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/provider"
	"errors"
	"fmt"
	"math/big"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CreateProviderTaskJobInput struct {
	Project         *models.Project
	APIType         models.TaskAPIType
	Route           ProviderModelRoute
	Background      bool
	Stream          bool
	RequestBody     []byte
	PriorityGwei    *big.Int
	CreditsToLock   *big.Int
	QueueTimeout    uint64
	BillingSnapshot models.ProviderTaskBillingData
	PublicID        string
}

func ReserveCreditsAndCreateProviderTaskJob(ctx context.Context, db *gorm.DB, in CreateProviderTaskJobInput) (*models.TaskJob, error) {
	if in.Project == nil {
		return nil, errors.New("project is required")
	}
	if in.PriorityGwei == nil || in.PriorityGwei.Sign() <= 0 {
		return nil, errors.New("priority_gwei is required")
	}
	if in.CreditsToLock == nil || in.CreditsToLock.Sign() <= 0 {
		return nil, errors.New("credits_to_lock must be positive")
	}
	if in.QueueTimeout == 0 {
		return nil, errors.New("queue_timeout is required")
	}
	taskFeeWei := ProviderEarningWei(in.PriorityGwei) // placeholder overwritten by estimate; store priority-based snapshot fee
	if billable, err := ProviderBillableGwei(in.PriorityGwei, big.NewRat(1, 1)); err == nil {
		taskFeeWei = ProviderEarningWei(billable)
	}
	billingData, err := models.EncodeTaskBillingData(models.TaskBillingData{
		Version:       models.TaskBillingDataVersion,
		PriorityGwei:  in.PriorityGwei.String(),
		BilledVram:    0,
		TaskFeeWei:    taskFeeWei.String(),
		LockedCredits: in.CreditsToLock.String(),
		Provider:      &in.BillingSnapshot,
	})
	if err != nil {
		return nil, err
	}
	phase := "queued"
	if in.Stream {
		// Streaming jobs stay invisible to the scheduler until the handler registers a subscriber.
		phase = "admitting"
	}
	job := models.TaskJob{
		ProjectID:         in.Project.ID,
		UserID:            in.Project.UserID,
		PriorityGwei:      models.BigInt{Int: *new(big.Int).Set(in.PriorityGwei)},
		SortablePriority:  models.FormatSortablePriority(in.PriorityGwei),
		ExecutionSource:   in.Route.ServiceProvider.ExecutionSource(),
		TaskType:          models.TaskTypeLLM,
		APIType:           in.APIType,
		Model:             in.Route.ModelName,
		BilledVram:        0,
		Background:        in.Background,
		Stream:            in.Stream,
		RequestBody:       in.RequestBody,
		TaskArgsJSON:      string(in.RequestBody),
		QueueTimeout:      in.QueueTimeout,
		Status:            models.TaskJobStatusPendingSubmit,
		Phase:             phase,
		BillingData:       billingData,
		CreditsLocked:     models.BigInt{Int: *new(big.Int).Set(in.CreditsToLock)},
		CreditsLockStatus: models.TaskJobCreditsLockHeld,
		PublicID:          in.PublicID,
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		if err := reserveCreditsTx(tx, in.Project.UserID, in.CreditsToLock); err != nil {
			return err
		}
		return tx.Create(&job).Error
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func SettleProviderTaskJob(
	ctx context.Context,
	db *gorm.DB,
	job *models.TaskJob,
	sharedAPIKeyID uint,
	keyOwnerUserID uint,
	keyOwnerAddress string,
	rawResultJSON string,
	formattedResultJSON string,
	usage provider.NormalizedUsage,
) error {
	billing, err := job.DecodeBillingData()
	if err != nil {
		return err
	}
	if billing.Provider == nil {
		return errors.New("missing provider billing snapshot")
	}
	components := ProviderUsageComponents{
		InputTokens:           usage.InputTokens,
		OutputTokens:          usage.OutputTokens,
		CachedInputTokens:     usage.CachedInputTokens,
		CacheWriteInputTokens: usage.CacheWriteInputTokens,
		ReasoningTokens:       usage.ReasoningTokens,
		ToolUsePromptTokens:   usage.ToolUsePromptTokens,
		TotalTokens:           usage.TotalTokens,
	}
	units, err := CalculateProviderUnits(components, billing.Provider.UsageWeights)
	if err != nil {
		return err
	}
	priority, ok := new(big.Int).SetString(billing.Provider.EffectivePriorityGwei, 10)
	if !ok {
		return errors.New("invalid effective priority in snapshot")
	}
	billableGwei, err := ProviderBillableGwei(priority, units)
	if err != nil {
		return err
	}
	credits, err := ProviderConsumerCredits(billableGwei, billing.Provider.CreditsPerGwei)
	if err != nil {
		return err
	}
	earningWei := ProviderEarningWei(billableGwei)

	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var locked models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, job.ID).Error; err != nil {
			return err
		}
		if locked.IsTerminal() {
			return nil
		}
		if locked.TaskCallRecordID != nil {
			return nil
		}
		if err := releaseCreditsHoldTx(tx, &locked); err != nil {
			return err
		}
		keyID := sharedAPIKeyID
		in := RecordTaskCallInput{
			UserID:               locked.UserID,
			ProjectID:            locked.ProjectID,
			TaskJobID:            &locked.ID,
			TaskType:             locked.TaskType,
			APIType:              locked.APIType,
			Model:                locked.Model,
			ExecutionSource:      locked.ExecutionSource,
			SharedAPIKeyID:       &keyID,
			TaskArgsJSON:         locked.TaskArgsJSON,
			PromptTokens:         usage.InputTokens,
			CompletionTokens:     usage.OutputTokens,
			TotalTokens:          usage.TotalTokens,
			PriorityGwei:         priority,
			TokenUsageApplicable: true,
			Status:               models.TaskCallStatusSuccess,
			Credits:              credits,
			AcceptedAt:           locked.CreatedAt,
			CompletedAt:          time.Now(),
			BilledVram:           0,
			Charge:               true,
		}
		recordID, err := processTaskCallTx(tx, in)
		if err != nil {
			return err
		}
		if err := CreateTaskIncomeEarningTx(tx, keyOwnerUserID, keyOwnerAddress, earningWei, recordID); err != nil {
			return err
		}
		now := time.Now()
		updates := map[string]interface{}{
			"status":                models.TaskJobStatusCompleted,
			"billing_status":        models.TaskJobBillingBilled,
			"task_call_record_id":   recordID,
			"raw_result_json":       rawResultJSON,
			"formatted_result_json": formattedResultJSON,
			"completed_at":          now,
			"phase":                 "completed",
		}
		return tx.Model(&locked).Updates(updates).Error
	})
}

func FailProviderTaskJob(ctx context.Context, db *gorm.DB, jobID uint, sharedAPIKeyID *uint, errorMessage string) error {
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var locked models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, jobID).Error; err != nil {
			return err
		}
		if locked.IsTerminal() {
			return nil
		}
		if err := releaseCreditsHoldTx(tx, &locked); err != nil {
			return err
		}
		in := RecordTaskCallInput{
			UserID:               locked.UserID,
			ProjectID:            locked.ProjectID,
			TaskJobID:            &locked.ID,
			TaskType:             locked.TaskType,
			APIType:              locked.APIType,
			Model:                locked.Model,
			ExecutionSource:      locked.ExecutionSource,
			SharedAPIKeyID:       sharedAPIKeyID,
			TaskArgsJSON:         locked.TaskArgsJSON,
			PriorityGwei:         &locked.PriorityGwei.Int,
			TokenUsageApplicable: false,
			Status:               models.TaskCallStatusFailed,
			Credits:              big.NewInt(0),
			AcceptedAt:           locked.CreatedAt,
			CompletedAt:          time.Now(),
			Charge:               false,
		}
		recordID, err := processTaskCallTx(tx, in)
		if err != nil {
			return err
		}
		now := time.Now()
		msg := errorMessage
		return tx.Model(&locked).Updates(map[string]interface{}{
			"status":              models.TaskJobStatusFailed,
			"billing_status":      models.TaskJobBillingNotBilled,
			"task_call_record_id": recordID,
			"error_message":       msg,
			"completed_at":        now,
			"phase":               "failed",
		}).Error
	})
}

func FailPriorProcessProviderJobs(ctx context.Context, db *gorm.DB) error {
	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var ids []uint
	if err := db.WithContext(dbCtx).Model(&models.TaskJob{}).
		Where("execution_source IN ? AND status IN ?",
			[]models.ExecutionSource{models.ExecutionSourceOpenAI, models.ExecutionSourceAnthropic, models.ExecutionSourceGoogle},
			[]models.TaskJobStatus{models.TaskJobStatusSubmitted, models.TaskJobStatusInProgress},
		).
		Order("id ASC").
		Limit(1000).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		if err := FailProviderTaskJob(ctx, db, id, nil, "provider job interrupted by process restart"); err != nil {
			log.Errorf("fail prior provider job %d: %v", id, err)
			return err
		}
	}
	return nil
}

func ResolveProviderQueueTimeout(requestTimeout *uint64) uint64 {
	if requestTimeout != nil && *requestTimeout > 0 {
		return *requestTimeout
	}
	return config.GetConfig().Provider.DefaultQueueTimeoutSeconds
}

func ResolveProviderEffectivePriority(project *models.Project) (*big.Int, error) {
	if project == nil {
		return nil, errors.New("project is required")
	}
	mode := project.CostLevelMode
	if mode == "" {
		mode = models.CostLevelModeStatic
	}
	if mode != models.CostLevelModeAuto {
		return ResolveEffectivePriorityGwei(project)
	}
	snapshot := GetProviderQueuePrioritySnapshot()
	if snapshot.HasJobs && snapshot.Highest != nil && snapshot.Lowest != nil && snapshot.Highest.Cmp(snapshot.Lowest) >= 0 {
		return ResolveAutoPriorityFromBounds(project, snapshot.Lowest, snapshot.Highest)
	}
	return config.GetConfig().ParseMinPriorityGwei()
}

func ResolveAutoPriorityFromBounds(project *models.Project, lowest, highest *big.Int) (*big.Int, error) {
	position := DefaultAutoQueuePosition
	if project.AutoQueuePosition != nil {
		position = *project.AutoQueuePosition
	}
	if position < 0 || position > 100 {
		return nil, fmt.Errorf("invalid auto queue position")
	}
	span := new(big.Int).Sub(highest, lowest)
	offset := new(big.Int).Mul(span, big.NewInt(int64(position)))
	offset.Div(offset, big.NewInt(100))
	effective := new(big.Int).Add(lowest, offset)
	if project.AutoMaxPriorityGwei != nil && project.AutoMaxPriorityGwei.Sign() > 0 && effective.Cmp(&project.AutoMaxPriorityGwei.Int) > 0 {
		effective = new(big.Int).Set(&project.AutoMaxPriorityGwei.Int)
	}
	return ClampPriorityGwei(effective)
}
