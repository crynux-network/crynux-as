package service

import (
	"context"
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
	Project              *models.Project
	APIType              models.LLMAPIType
	Model                string
	BilledVram           uint64
	Background           bool
	Stream               bool
	RequestBody          []byte
	TaskArgsJSON         string
	TaskFeeGwei          *big.Int
	MedianPriorityGwei   *big.Int
	EstimatedNodeSeconds *float64
	VramWeight           *float64
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

func SettleLLMJob(
	ctx context.Context,
	db *gorm.DB,
	job *models.LLMJob,
	project *models.Project,
	vramRatio uint,
	prices LLMPrices,
) error {
	if job == nil {
		return errors.New("job is required")
	}
	if project == nil {
		return errors.New("project is required")
	}
	if job.BillingStatus == models.LLMJobBillingBilled {
		return nil
	}
	if job.Status != models.LLMJobStatusCompleted {
		return errors.New("only completed jobs can be settled")
	}

	credits := CalcCredits(job.PromptTokens, job.CompletionTokens, project.TokenRatio, vramRatio, prices)
	durationMs := uint64(0)
	if job.StartedAt != nil && job.CompletedAt != nil {
		durationMs = uint64(job.CompletedAt.Sub(*job.StartedAt).Milliseconds())
	}

	in := RecordLLMCallInput{
		UserID:           project.UserID,
		ProjectID:        project.ID,
		Model:            job.Model,
		PromptTokens:     job.PromptTokens,
		CompletionTokens: job.CompletionTokens,
		TotalTokens:      job.TotalTokens,
		TokenRatio:       project.TokenRatio,
		Status:           models.LLMCallStatusSuccess,
		Credits:          credits,
		DurationMs:       durationMs,
		BilledVram:       job.BilledVram,
		Charge:           true,
	}
	if job.TaskFeeGwei != nil {
		in.TaskFeeGwei = &job.TaskFeeGwei.Int
	}
	if job.MedianPriorityGwei != nil {
		in.MedianPriorityGwei = &job.MedianPriorityGwei.Int
	}
	in.EstimatedNodeSeconds = cloneFloat64Ptr(job.EstimatedNodeSeconds)
	in.VramWeight = cloneFloat64Ptr(job.VramWeight)

	recordID, err := ProcessLLMCall(ctx, db, in)
	if err != nil {
		return err
	}

	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return db.WithContext(dbCtx).Model(job).Updates(map[string]interface{}{
		"billing_status":     models.LLMJobBillingBilled,
		"llm_call_record_id": recordID,
	}).Error
}

func CompleteLLMJob(
	ctx context.Context,
	db *gorm.DB,
	jobID uint,
	rawResultJSON string,
	formattedResultJSON string,
	promptTokens, completionTokens, totalTokens uint64,
) (*models.LLMJob, error) {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updates := map[string]interface{}{
		"status":                models.LLMJobStatusCompleted,
		"raw_result_json":       rawResultJSON,
		"formatted_result_json": formattedResultJSON,
		"prompt_tokens":         promptTokens,
		"completion_tokens":     completionTokens,
		"total_tokens":          totalTokens,
		"completed_at":          now,
	}
	if err := db.WithContext(dbCtx).Model(&models.LLMJob{}).Where("id = ?", jobID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return GetLLMJobByID(ctx, db, jobID)
}

func FailLLMJob(ctx context.Context, db *gorm.DB, jobID uint, errorMessage string) (*models.LLMJob, error) {
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updates := map[string]interface{}{
		"status":          models.LLMJobStatusFailed,
		"error_message":   errorMessage,
		"billing_status":  models.LLMJobBillingNotBilled,
		"completed_at":    now,
	}
	if err := db.WithContext(dbCtx).Model(&models.LLMJob{}).Where("id = ?", jobID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return GetLLMJobByID(ctx, db, jobID)
}

func ClaimNextLLMJob(ctx context.Context, db *gorm.DB) (*models.LLMJob, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var job models.LLMJob
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status IN ?", []models.LLMJobStatus{
				models.LLMJobStatusPendingSubmit,
				models.LLMJobStatusSubmitted,
				models.LLMJobStatusInProgress,
			}).
			Order("created_at ASC")

		if err := query.First(&job).Error; err != nil {
			return err
		}

		if job.Status == models.LLMJobStatusPendingSubmit {
			return tx.Model(&job).Update("status", models.LLMJobStatusSubmitted).Error
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	if job.Status == models.LLMJobStatusPendingSubmit {
		job.Status = models.LLMJobStatusSubmitted
	}
	return &job, nil
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
		"status":               models.LLMJobStatusPendingSubmit,
		"bridge_client_task_id": nil,
		"started_at":           nil,
	}).Error
}
