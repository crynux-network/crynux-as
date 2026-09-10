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
	ErrLLMJobNotFound    = errors.New("llm job not found")
	ErrLLMJobWaitTimeout = errors.New("llm job wait timeout")
)

type CreateLLMJobInput struct {
	Project               *models.Project
	APIType               models.LLMAPIType
	Model                 string
	BilledVram            uint64
	Background            bool
	Stream                bool
	RequestBody           []byte
	TaskArgsJSON          string
	TaskFeeGwei           *big.Int
	MedianPriorityGwei    *big.Int
	EstimatedNodeSeconds  *float64
	VramWeight            *float64
	ConstantSeconds       *float64
	SecondsPerInputToken  *float64
	SecondsPerOutputToken *float64
}

func CreateLLMJob(ctx context.Context, db *gorm.DB, in CreateLLMJobInput) (*models.LLMJob, error) {
	if in.Project == nil {
		return nil, errors.New("project is required")
	}
	if in.Model == "" {
		return nil, errors.New("model is required")
	}
	if in.TaskArgsJSON == "" {
		return nil, errors.New("task args json is required")
	}

	job := models.LLMJob{
		ProjectID:    in.Project.ID,
		UserID:       in.Project.UserID,
		TokenRatio:   in.Project.TokenRatio,
		APIType:      in.APIType,
		Model:        in.Model,
		BilledVram:   in.BilledVram,
		Background:   in.Background,
		Stream:       in.Stream,
		RequestBody:  in.RequestBody,
		TaskArgsJSON: in.TaskArgsJSON,
		Status:       models.LLMJobStatusPendingSubmit,
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

	if in.APIType == models.LLMAPITypeResponses {
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

func GetLLMJobByPublicID(ctx context.Context, db *gorm.DB, projectID uint, publicID string) (*models.LLMJob, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var job models.LLMJob
	err := db.WithContext(dbCtx).
		Where("project_id = ? AND public_id = ?", projectID, publicID).
		First(&job).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLLMJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func GetLLMJobByID(ctx context.Context, db *gorm.DB, jobID uint) (*models.LLMJob, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var job models.LLMJob
	err := db.WithContext(dbCtx).First(&job, jobID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrLLMJobNotFound
		}
		return nil, err
	}
	return &job, nil
}

func WaitForLLMJob(ctx context.Context, db *gorm.DB, jobID uint, timeout time.Duration) (*models.LLMJob, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		job, err := GetLLMJobByID(ctx, db, jobID)
		if err != nil {
			return nil, err
		}
		if job.IsTerminal() {
			return job, nil
		}
		if time.Now().After(deadline) {
			return job, ErrLLMJobWaitTimeout
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func CompleteAndSettleLLMJob(
	ctx context.Context,
	db *gorm.DB,
	job *models.LLMJob,
	rawResultJSON string,
	formattedResultJSON string,
	promptTokens, completionTokens, totalTokens uint64,
) (*models.LLMJob, error) {
	if job == nil {
		return nil, errors.New("job is required")
	}
	if job.Status == models.LLMJobStatusCompleted && job.BillingStatus == models.LLMJobBillingBilled {
		return job, nil
	}
	if job.UserID == 0 || job.TokenRatio == 0 {
		project, err := loadProjectByID(ctx, db, job.ProjectID)
		if err != nil {
			return nil, err
		}
		if job.UserID == 0 {
			job.UserID = project.UserID
		}
		if job.TokenRatio == 0 {
			job.TokenRatio = project.TokenRatio
		}
	}
	if job.UserID == 0 {
		return nil, errors.New("job user_id is required")
	}
	if job.ConstantSeconds == nil || job.SecondsPerInputToken == nil || job.SecondsPerOutputToken == nil {
		return nil, errors.New("job execution-time coefficients are required")
	}
	if job.VramWeight == nil {
		return nil, errors.New("job vram_weight is required")
	}

	appCfg := config.GetConfig()
	referencePriority, err := appCfg.ParseReferencePriorityGwei()
	if err != nil {
		return nil, fmt.Errorf("parse reference priority: %w", err)
	}

	credits, err := CalcCredits(
		promptTokens,
		completionTokens,
		job.TokenRatio,
		*job.VramWeight,
		*job.ConstantSeconds,
		*job.SecondsPerInputToken,
		*job.SecondsPerOutputToken,
		referencePriority,
		appCfg.LLM.CreditsPerGwei,
	)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	creditsPerGwei := appCfg.LLM.CreditsPerGwei
	err = db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var locked models.LLMJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&locked, job.ID).Error; err != nil {
			return err
		}
		if locked.Status == models.LLMJobStatusCompleted && locked.BillingStatus == models.LLMJobBillingBilled {
			*job = locked
			return nil
		}
		if locked.LLMCallRecordID != nil {
			return fmt.Errorf("llm job %d already has call record %d", locked.ID, *locked.LLMCallRecordID)
		}

		jobID := locked.ID
		userID := locked.UserID
		tokenRatio := locked.TokenRatio
		if userID == 0 {
			userID = job.UserID
		}
		if tokenRatio == 0 {
			tokenRatio = job.TokenRatio
		}
		in := RecordLLMCallInput{
			UserID:                userID,
			ProjectID:             locked.ProjectID,
			LLMJobID:              &jobID,
			Model:                 locked.Model,
			PromptTokens:          promptTokens,
			CompletionTokens:      completionTokens,
			TotalTokens:           totalTokens,
			TokenRatio:            tokenRatio,
			TokenUsageApplicable:  true,
			Status:                models.LLMCallStatusSuccess,
			Credits:               credits,
			AcceptedAt:            locked.CreatedAt,
			CompletedAt:           now,
			BilledVram:            locked.BilledVram,
			ConstantSeconds:       cloneFloat64Ptr(locked.ConstantSeconds),
			SecondsPerInputToken:  cloneFloat64Ptr(locked.SecondsPerInputToken),
			SecondsPerOutputToken: cloneFloat64Ptr(locked.SecondsPerOutputToken),
			ReferencePriorityGwei: referencePriority,
			CreditsPerGwei:        &creditsPerGwei,
			Charge:                true,
		}
		if locked.TaskFeeGwei != nil {
			in.TaskFeeGwei = &locked.TaskFeeGwei.Int
		}
		if locked.MedianPriorityGwei != nil {
			in.MedianPriorityGwei = &locked.MedianPriorityGwei.Int
		}
		in.EstimatedNodeSeconds = cloneFloat64Ptr(locked.EstimatedNodeSeconds)
		in.VramWeight = cloneFloat64Ptr(locked.VramWeight)

		recordID, err := processLLMCallTx(tx, in)
		if err != nil {
			return err
		}

		updates := map[string]interface{}{
			"status":                models.LLMJobStatusCompleted,
			"raw_result_json":       rawResultJSON,
			"formatted_result_json": formattedResultJSON,
			"completed_at":          now,
			"billing_status":        models.LLMJobBillingBilled,
			"llm_call_record_id":    recordID,
		}
		if err := tx.Model(&models.LLMJob{}).Where("id = ?", locked.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(job, locked.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func FailAndRecordLLMJob(ctx context.Context, db *gorm.DB, job *models.LLMJob, errorMessage string) (*models.LLMJob, error) {
	if job == nil {
		return nil, errors.New("job is required")
	}
	if job.UserID == 0 || job.TokenRatio == 0 {
		project, err := loadProjectByID(ctx, db, job.ProjectID)
		if err != nil {
			return nil, err
		}
		if job.UserID == 0 {
			job.UserID = project.UserID
		}
		if job.TokenRatio == 0 {
			job.TokenRatio = project.TokenRatio
		}
	}
	if job.UserID == 0 {
		return nil, errors.New("job user_id is required")
	}

	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var locked models.LLMJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&locked, job.ID).Error; err != nil {
			return err
		}
		if locked.IsTerminal() && locked.LLMCallRecordID != nil {
			*job = locked
			return nil
		}

		jobID := locked.ID
		userID := locked.UserID
		tokenRatio := locked.TokenRatio
		if userID == 0 {
			userID = job.UserID
		}
		if tokenRatio == 0 {
			tokenRatio = job.TokenRatio
		}
		in := RecordLLMCallInput{
			UserID:                userID,
			ProjectID:             locked.ProjectID,
			LLMJobID:              &jobID,
			Model:                 locked.Model,
			TokenRatio:            tokenRatio,
			TokenUsageApplicable:  true,
			Status:                models.LLMCallStatusFailed,
			Credits:               big.NewInt(0),
			AcceptedAt:            locked.CreatedAt,
			CompletedAt:           now,
			BilledVram:            locked.BilledVram,
			ConstantSeconds:       cloneFloat64Ptr(locked.ConstantSeconds),
			SecondsPerInputToken:  cloneFloat64Ptr(locked.SecondsPerInputToken),
			SecondsPerOutputToken: cloneFloat64Ptr(locked.SecondsPerOutputToken),
			Charge:                false,
		}
		if locked.TaskFeeGwei != nil {
			in.TaskFeeGwei = &locked.TaskFeeGwei.Int
		}
		if locked.MedianPriorityGwei != nil {
			in.MedianPriorityGwei = &locked.MedianPriorityGwei.Int
		}
		in.EstimatedNodeSeconds = cloneFloat64Ptr(locked.EstimatedNodeSeconds)
		in.VramWeight = cloneFloat64Ptr(locked.VramWeight)

		recordID, err := processLLMCallTx(tx, in)
		if err != nil {
			return err
		}

		updates := map[string]interface{}{
			"status":             models.LLMJobStatusFailed,
			"error_message":      errorMessage,
			"billing_status":     models.LLMJobBillingNotBilled,
			"completed_at":       now,
			"llm_call_record_id": recordID,
		}
		if err := tx.Model(&models.LLMJob{}).Where("id = ?", locked.ID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(job, locked.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return job, nil
}

// FailLLMJob marks a job failed without writing a call record.
// Prefer FailAndRecordLLMJob for terminal failures that must enter usage stats.
func FailLLMJob(ctx context.Context, db *gorm.DB, jobID uint, errorMessage string) (*models.LLMJob, error) {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updates := map[string]interface{}{
		"status":         models.LLMJobStatusFailed,
		"error_message":  errorMessage,
		"billing_status": models.LLMJobBillingNotBilled,
		"completed_at":   now,
	}
	if err := db.WithContext(dbCtx).Model(&models.LLMJob{}).Where("id = ?", jobID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return GetLLMJobByID(ctx, db, jobID)
}

func ListUnfinishedLLMJobs(ctx context.Context, db *gorm.DB, limit int) ([]models.LLMJob, error) {
	if limit <= 0 {
		limit = 100
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var jobs []models.LLMJob
	err := db.WithContext(dbCtx).
		Where("status IN ?", []models.LLMJobStatus{
			models.LLMJobStatusPendingSubmit,
			models.LLMJobStatusSubmitted,
			models.LLMJobStatusInProgress,
		}).
		Order("created_at ASC").
		Limit(limit).
		Find(&jobs).Error
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

func UpdateLLMJobBridgeTask(ctx context.Context, db *gorm.DB, jobID uint, bridgeClientTaskID uint) error {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Model(&models.LLMJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"bridge_client_task_id": bridgeClientTaskID,
		"status":                models.LLMJobStatusSubmitted,
		"started_at":            now,
	}).Error
}

func UpdateLLMJobStatus(ctx context.Context, db *gorm.DB, jobID uint, status models.LLMJobStatus) error {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Model(&models.LLMJob{}).Where("id = ?", jobID).Update("status", status).Error
}

func ResetLLMJobToPending(ctx context.Context, db *gorm.DB, jobID uint) error {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Model(&models.LLMJob{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"status":                models.LLMJobStatusPendingSubmit,
		"bridge_client_task_id": nil,
		"started_at":            nil,
	}).Error
}

const llmJobCleanupBatchSize = 200

// DeleteExpiredTerminalLLMJobs deletes one bounded batch of terminal jobs whose
// completed_at is older than retentionDays and that already have a call record.
// Only completed+billed and failed+not_billed jobs are selected.
func DeleteExpiredTerminalLLMJobs(ctx context.Context, db *gorm.DB, retentionDays uint64) (int, error) {
	if retentionDays == 0 {
		return 0, errors.New("retention days must be positive")
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

	dbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var deleted int
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		selectBatch := func(status models.LLMJobStatus, billing models.LLMJobBillingStatus) error {
			var batch []uint
			if err := tx.Model(&models.LLMJob{}).
				Select("id").
				Where("status = ? AND billing_status = ?", status, billing).
				Where("llm_call_record_id IS NOT NULL").
				Where("completed_at IS NOT NULL").
				Where("completed_at < ?", cutoff).
				Order("completed_at ASC, id ASC").
				Limit(llmJobCleanupBatchSize).
				Pluck("id", &batch).Error; err != nil {
				return err
			}
			ids = append(ids, batch...)
			return nil
		}
		if err := selectBatch(models.LLMJobStatusCompleted, models.LLMJobBillingBilled); err != nil {
			return err
		}
		if err := selectBatch(models.LLMJobStatusFailed, models.LLMJobBillingNotBilled); err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if len(ids) > llmJobCleanupBatchSize {
			ids = ids[:llmJobCleanupBatchSize]
		}
		result := tx.Where("id IN ?", ids).Delete(&models.LLMJob{})
		if result.Error != nil {
			return result.Error
		}
		deleted = int(result.RowsAffected)
		return nil
	})
	return deleted, err
}

// RunLLMJobRetentionCleanup repeatedly deletes expired terminal jobs until a
// batch deletes fewer than the batch size or an error occurs.
func RunLLMJobRetentionCleanup(ctx context.Context, db *gorm.DB, retentionDays uint64) (int, error) {
	total := 0
	for {
		deleted, err := DeleteExpiredTerminalLLMJobs(ctx, db, retentionDays)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < llmJobCleanupBatchSize {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
	}
}

// LLMJobRetentionCutoff returns the earliest completed_at that remains readable
// under the configured retention window.
func LLMJobRetentionCutoff(retentionDays uint64) time.Time {
	return time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
}

