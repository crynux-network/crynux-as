package service

import (
	"context"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/utils"
	"errors"
	"fmt"
	"math/big"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTaskJobNotFound    = errors.New("task job not found")
	ErrTaskJobWaitTimeout = errors.New("task job wait timeout")
)

type CreateTaskJobInput struct {
	Project               *models.Project
	APIType               models.TaskAPIType
	Model                 string
	BilledVram            uint64
	Background            bool
	Stream                bool
	RequestBody           []byte
	TaskArgsJSON          string
	PriorityGwei          *big.Int
	TaskFeeGwei           *big.Int
	MedianPriorityGwei    *big.Int
	EstimatedNodeSeconds  *float64
	VramWeight            *float64
	ConstantSeconds       *float64
	SecondsPerInputToken  *float64
	SecondsPerOutputToken *float64
}

func CreateTaskJob(ctx context.Context, db *gorm.DB, in CreateTaskJobInput) (*models.TaskJob, error) {
	if in.Project == nil {
		return nil, errors.New("project is required")
	}
	if in.Model == "" {
		return nil, errors.New("model is required")
	}
	if in.TaskArgsJSON == "" {
		return nil, errors.New("task args json is required")
	}
	if in.PriorityGwei == nil || in.PriorityGwei.Sign() <= 0 {
		return nil, errors.New("priority_gwei is required")
	}
	if in.TaskFeeGwei == nil || in.TaskFeeGwei.Sign() < 0 {
		return nil, errors.New("task_fee_gwei is required")
	}
	if in.VramWeight == nil || in.ConstantSeconds == nil || in.SecondsPerInputToken == nil || in.SecondsPerOutputToken == nil {
		return nil, errors.New("llm billing data is required")
	}
	taskFeeWei := new(big.Int).Mul(in.TaskFeeGwei, big.NewInt(1_000_000_000))
	billingData, err := models.EncodeTaskBillingData(models.TaskBillingData{
		Version:      models.TaskBillingDataVersion,
		PriorityGwei: in.PriorityGwei.String(),
		BilledVram:   in.BilledVram,
		TaskFeeWei:   taskFeeWei.String(),
		LLM: &models.LLMTaskBillingData{
			VramWeight:            *in.VramWeight,
			ConstantSeconds:       *in.ConstantSeconds,
			SecondsPerInputToken:  *in.SecondsPerInputToken,
			SecondsPerOutputToken: *in.SecondsPerOutputToken,
			CreditsPerGwei:        config.GetConfig().LLM.CreditsPerGwei,
		},
	})
	if err != nil {
		return nil, err
	}
	job := models.TaskJob{
		ProjectID:    in.Project.ID,
		UserID:       in.Project.UserID,
		PriorityGwei: models.BigInt{Int: *new(big.Int).Set(in.PriorityGwei)},
		TaskType:     models.TaskTypeLLM,
		APIType:      in.APIType,
		Model:        in.Model,
		BilledVram:   in.BilledVram,
		MinVram:      &in.BilledVram,
		Background:   in.Background,
		Stream:       in.Stream,
		RequestBody:  in.RequestBody,
		TaskArgsJSON: in.TaskArgsJSON,
		Status:       models.TaskJobStatusPendingSubmit,
		BillingData:  billingData,
	}
	if in.TaskFeeGwei != nil {
		job.TaskFeeGwei = &models.BigInt{Int: *new(big.Int).Set(in.TaskFeeGwei)}
	}
	if in.MedianPriorityGwei != nil {
		job.MedianPriorityGwei = &models.BigInt{Int: *new(big.Int).Set(in.MedianPriorityGwei)}
	}
	job.EstimatedNodeSeconds = cloneFloat64Ptr(in.EstimatedNodeSeconds)
	job.VramWeight = cloneFloat64Ptr(in.VramWeight)
	job.ConstantSeconds = cloneFloat64Ptr(in.ConstantSeconds)
	job.SecondsPerInputToken = cloneFloat64Ptr(in.SecondsPerInputToken)
	job.SecondsPerOutputToken = cloneFloat64Ptr(in.SecondsPerOutputToken)

	if in.APIType == models.TaskAPITypeResponses {
		token, err := utils.GenerateRandomToken(16)
		if err != nil {
			return nil, fmt.Errorf("generate response public id: %w", err)
		}
		job.PublicID = "resp_" + token
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.WithContext(dbCtx).Create(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func GetTaskJobByPublicID(ctx context.Context, db *gorm.DB, projectID uint, publicID string) (*models.TaskJob, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var job models.TaskJob
	err := db.WithContext(dbCtx).
		Where("project_id = ? AND public_id = ?", projectID, publicID).
		First(&job).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func GetTaskJobByID(ctx context.Context, db *gorm.DB, jobID uint) (*models.TaskJob, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var job models.TaskJob
	err := db.WithContext(dbCtx).First(&job, jobID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func WaitForTaskJob(ctx context.Context, db *gorm.DB, jobID uint, timeout time.Duration) (*models.TaskJob, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		job, err := GetTaskJobByID(ctx, db, jobID)
		if err != nil {
			return nil, err
		}
		if job.IsTerminal() {
			return job, nil
		}
		if time.Now().After(deadline) {
			return job, ErrTaskJobWaitTimeout
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func fillJobUserAndPriority(ctx context.Context, db *gorm.DB, job *models.TaskJob) error {
	if job.UserID != 0 && job.PriorityGwei.Sign() != 0 {
		return nil
	}
	project, err := loadProjectByID(ctx, db, job.ProjectID)
	if err != nil {
		return err
	}
	if job.UserID == 0 {
		job.UserID = project.UserID
	}
	if job.PriorityGwei.Sign() == 0 {
		effective, err := ResolveEffectivePriorityGwei(project)
		if err != nil {
			return err
		}
		job.PriorityGwei = models.BigInt{Int: *effective}
	}
	return nil
}

func CompleteAndSettleTaskJob(
	ctx context.Context,
	db *gorm.DB,
	job *models.TaskJob,
	rawResultJSON string,
	formattedResultJSON string,
	promptTokens, completionTokens, totalTokens uint64,
) (*models.TaskJob, error) {
	if job == nil {
		return nil, errors.New("job is required")
	}
	if job.Status == models.TaskJobStatusCompleted && job.BillingStatus == models.TaskJobBillingBilled {
		return job, nil
	}
	if err := fillJobUserAndPriority(ctx, db, job); err != nil {
		return nil, err
	}
	if job.UserID == 0 {
		return nil, errors.New("job user_id is required")
	}
	billing, err := job.DecodeBillingData()
	if err != nil {
		return nil, fmt.Errorf("decode billing data: %w", err)
	}
	if billing.LLM == nil {
		return nil, errors.New("job llm billing data is required")
	}
	priorityGwei, _ := new(big.Int).SetString(billing.PriorityGwei, 10)
	credits, err := CalcCredits(
		promptTokens,
		completionTokens,
		priorityGwei,
		billing.LLM.VramWeight,
		billing.LLM.ConstantSeconds,
		billing.LLM.SecondsPerInputToken,
		billing.LLM.SecondsPerOutputToken,
		billing.LLM.CreditsPerGwei,
	)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err = db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var locked models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&locked, job.ID).Error; err != nil {
			return err
		}
		if locked.Status == models.TaskJobStatusCompleted && locked.BillingStatus == models.TaskJobBillingBilled {
			*job = locked
			return nil
		}
		if locked.TaskCallRecordID != nil {
			return fmt.Errorf("task job %d already has call record %d", locked.ID, *locked.TaskCallRecordID)
		}

		lockedBilling, err := locked.DecodeBillingData()
		if err != nil {
			return err
		}
		lockedPriority, _ := new(big.Int).SetString(lockedBilling.PriorityGwei, 10)
		jobID := locked.ID
		userID := locked.UserID
		if userID == 0 {
			userID = job.UserID
		}
		in := RecordTaskCallInput{
			UserID:               userID,
			ProjectID:            locked.ProjectID,
			TaskJobID:            &jobID,
			TaskType:             locked.TaskType,
			APIType:              locked.APIType,
			Model:                locked.Model,
			PromptTokens:         promptTokens,
			CompletionTokens:     completionTokens,
			TotalTokens:          totalTokens,
			PriorityGwei:         lockedPriority,
			TokenUsageApplicable: true,
			Status:               models.TaskCallStatusSuccess,
			Credits:              credits,
			AcceptedAt:           locked.CreatedAt,
			CompletedAt:          now,
			BilledVram:           lockedBilling.BilledVram,
			Charge:               true,
		}

		recordID, err := processTaskCallTx(tx, in)
		if err != nil {
			return err
		}

		updates := map[string]interface{}{
			"status":                models.TaskJobStatusCompleted,
			"raw_result_json":       rawResultJSON,
			"formatted_result_json": formattedResultJSON,
			"completed_at":          now,
			"billing_status":        models.TaskJobBillingBilled,
			"task_call_record_id":   recordID,
		}
		if err := tx.Model(&models.TaskJob{}).Where("id = ?", locked.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(job, locked.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func FailAndRecordTaskJob(ctx context.Context, db *gorm.DB, job *models.TaskJob, errorMessage string) (*models.TaskJob, error) {
	if job == nil {
		return nil, errors.New("job is required")
	}
	if err := fillJobUserAndPriority(ctx, db, job); err != nil {
		return nil, err
	}
	if job.UserID == 0 {
		return nil, errors.New("job user_id is required")
	}

	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var locked models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&locked, job.ID).Error; err != nil {
			return err
		}
		if locked.IsTerminal() && locked.TaskCallRecordID != nil {
			*job = locked
			return nil
		}

		lockedBilling, err := locked.DecodeBillingData()
		if err != nil {
			return err
		}
		lockedPriority, _ := new(big.Int).SetString(lockedBilling.PriorityGwei, 10)
		jobID := locked.ID
		userID := locked.UserID
		if userID == 0 {
			userID = job.UserID
		}
		in := RecordTaskCallInput{
			UserID:               userID,
			ProjectID:            locked.ProjectID,
			TaskJobID:            &jobID,
			TaskType:             locked.TaskType,
			APIType:              locked.APIType,
			Model:                locked.Model,
			PriorityGwei:         lockedPriority,
			TokenUsageApplicable: locked.TaskType == models.TaskTypeLLM,
			Status:               models.TaskCallStatusFailed,
			Credits:              big.NewInt(0),
			AcceptedAt:           locked.CreatedAt,
			CompletedAt:          now,
			BilledVram:           lockedBilling.BilledVram,
			Charge:               false,
		}

		recordID, err := processTaskCallTx(tx, in)
		if err != nil {
			return err
		}

		updates := map[string]interface{}{
			"status":              models.TaskJobStatusFailed,
			"error_message":       errorMessage,
			"billing_status":      models.TaskJobBillingNotBilled,
			"completed_at":        now,
			"task_call_record_id": recordID,
		}
		if err := tx.Model(&models.TaskJob{}).Where("id = ?", locked.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(job, locked.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

// FailTaskJob marks a job failed without writing a call record.
// Prefer FailAndRecordTaskJob for terminal failures that must enter usage stats.
func FailTaskJob(ctx context.Context, db *gorm.DB, jobID uint, errorMessage string) (*models.TaskJob, error) {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updates := map[string]interface{}{
		"status":         models.TaskJobStatusFailed,
		"error_message":  errorMessage,
		"billing_status": models.TaskJobBillingNotBilled,
		"completed_at":   now,
	}
	if err := db.WithContext(dbCtx).Model(&models.TaskJob{}).Where("id = ?", jobID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return GetTaskJobByID(ctx, db, jobID)
}

func ListUnfinishedTaskJobs(ctx context.Context, db *gorm.DB, limit int) ([]models.TaskJob, error) {
	if limit <= 0 {
		limit = 100
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var jobs []models.TaskJob
	err := db.WithContext(dbCtx).
		Where("status IN ?", []models.TaskJobStatus{
			models.TaskJobStatusPendingSubmit,
			models.TaskJobStatusSubmitted,
			models.TaskJobStatusInProgress,
		}).
		Order("created_at ASC").
		Limit(limit).
		Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func UpdateTaskJobBridgeTask(ctx context.Context, db *gorm.DB, jobID uint, bridgeClientTaskID uint) error {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Model(&models.TaskJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"bridge_client_task_id": bridgeClientTaskID,
		"status":                models.TaskJobStatusSubmitted,
		"started_at":            now,
	}).Error
}

func UpdateTaskJobStatus(ctx context.Context, db *gorm.DB, jobID uint, status models.TaskJobStatus) error {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Model(&models.TaskJob{}).Where("id = ?", jobID).Update("status", status).Error
}

func ResetTaskJobToPending(ctx context.Context, db *gorm.DB, jobID uint) error {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Model(&models.TaskJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"status":                models.TaskJobStatusPendingSubmit,
		"bridge_client_task_id": nil,
		"started_at":            nil,
	}).Error
}

const taskJobCleanupBatchSize = 200

// DeleteExpiredTerminalTaskJobs deletes one bounded batch of terminal jobs whose
// completed_at is older than retentionDays and that already have a call record.
// Only completed+billed and failed+not_billed jobs are selected.
func DeleteExpiredTerminalTaskJobs(ctx context.Context, db *gorm.DB, retentionDays uint64) (int, error) {
	if retentionDays == 0 {
		return 0, errors.New("retention days must be positive")
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var deleted int
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		selectBatch := func(status models.TaskJobStatus, billing models.TaskJobBillingStatus) error {
			var batch []uint
			if err := tx.Model(&models.TaskJob{}).
				Select("id").
				Where("status = ? AND billing_status = ?", status, billing).
				Where("task_call_record_id IS NOT NULL").
				Where("completed_at IS NOT NULL").
				Where("completed_at < ?", cutoff).
				Order("completed_at ASC, id ASC").
				Limit(taskJobCleanupBatchSize).
				Pluck("id", &batch).Error; err != nil {
				return err
			}
			ids = append(ids, batch...)
			return nil
		}
		if err := selectBatch(models.TaskJobStatusCompleted, models.TaskJobBillingBilled); err != nil {
			return err
		}
		if err := selectBatch(models.TaskJobStatusFailed, models.TaskJobBillingNotBilled); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if len(ids) > taskJobCleanupBatchSize {
			ids = ids[:taskJobCleanupBatchSize]
		}
		result := tx.Where("id IN ?", ids).Delete(&models.TaskJob{})
		if result.Error != nil {
			return result.Error
		}
		deleted = int(result.RowsAffected)
		return nil
	})
	return deleted, err
}

// RunTaskJobRetentionCleanup repeatedly deletes expired terminal jobs until a
// batch deletes fewer than the batch size or an error occurs.
func RunTaskJobRetentionCleanup(ctx context.Context, db *gorm.DB, retentionDays uint64) (int, error) {
	total := 0
	for {
		deleted, err := DeleteExpiredTerminalTaskJobs(ctx, db, retentionDays)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < taskJobCleanupBatchSize {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
	}
}

// TaskJobRetentionCutoff returns the earliest completed_at that remains readable
// under the configured retention window.
func TaskJobRetentionCutoff(retentionDays uint64) time.Time {
	return time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
}
