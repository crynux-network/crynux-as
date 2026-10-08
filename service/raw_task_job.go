package service

import (
	"context"
	"crynux_as/models"
	"errors"
	"math/big"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CreateRawTaskJobInput struct {
	Project         *models.Project
	TaskType        models.TaskType
	Model           string
	TaskArgsJSON    string
	TaskVersion     *string
	MinVram         *uint64
	RequiredGPU     string
	RequiredGPUVram uint64
	PriorityGwei    *big.Int
	BilledVram      uint64
	TaskFeeWei      *big.Int
	LLMBilling      *models.LLMTaskBillingData
	ImageCredits    *big.Int
}

func CreateRawTaskJob(ctx context.Context, db *gorm.DB, in CreateRawTaskJobInput) (*models.TaskJob, error) {
	if in.Project == nil {
		return nil, errors.New("project is required")
	}
	if in.TaskType != models.TaskTypeLLM && in.TaskType != models.TaskTypeImage {
		return nil, errors.New("task_type must be 0 or 1")
	}
	if in.Model == "" || in.TaskArgsJSON == "" {
		return nil, errors.New("model and task_args are required")
	}
	if in.PriorityGwei == nil || in.PriorityGwei.Sign() <= 0 {
		return nil, errors.New("priority_gwei is required")
	}
	if in.TaskFeeWei == nil || in.TaskFeeWei.Sign() < 0 {
		return nil, errors.New("task_fee_wei is required")
	}
	if in.BilledVram == 0 {
		return nil, errors.New("billed_vram must be positive")
	}
	billing := models.TaskBillingData{
		Version:      models.TaskBillingDataVersion,
		PriorityGwei: in.PriorityGwei.String(),
		BilledVram:   in.BilledVram,
		TaskFeeWei:   in.TaskFeeWei.String(),
		LLM:          in.LLMBilling,
	}
	if in.TaskType == models.TaskTypeImage {
		if in.ImageCredits == nil || in.ImageCredits.Sign() <= 0 {
			return nil, errors.New("image settlement credits are required")
		}
		billing.Image = &models.ImageTaskBillingData{SettlementCredits: in.ImageCredits.String()}
	}
	billingJSON, err := models.EncodeTaskBillingData(billing)
	if err != nil {
		return nil, err
	}
	job := models.TaskJob{
		ProjectID:       in.Project.ID,
		UserID:          in.Project.UserID,
		PriorityGwei:    models.BigInt{Int: *new(big.Int).Set(in.PriorityGwei)},
		TaskType:        in.TaskType,
		APIType:         models.TaskAPITypeRaw,
		Model:           in.Model,
		BilledVram:      in.BilledVram,
		TaskArgsJSON:    in.TaskArgsJSON,
		TaskVersion:     in.TaskVersion,
		MinVram:         in.MinVram,
		RequiredGPU:     in.RequiredGPU,
		RequiredGPUVram: in.RequiredGPUVram,
		Status:          models.TaskJobStatusPendingSubmit,
		BillingStatus:   models.TaskJobBillingPending,
		BillingData:     billingJSON,
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.WithContext(dbCtx).Create(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func CompleteAndSettleImageTaskJob(ctx context.Context, db *gorm.DB, job *models.TaskJob) (*models.TaskJob, error) {
	if job == nil {
		return nil, errors.New("job is required")
	}
	now := time.Now()
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := db.WithContext(dbCtx).Transaction(func(tx *gorm.DB) error {
		var locked models.TaskJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, job.ID).Error; err != nil {
			return err
		}
		if locked.IsTerminal() && locked.TaskCallRecordID != nil {
			*job = locked
			return nil
		}
		billing, err := locked.DecodeBillingData()
		if err != nil {
			return err
		}
		credits, ok := new(big.Int).SetString(billing.Image.SettlementCredits, 10)
		if !ok || credits.Sign() <= 0 {
			return errors.New("invalid image settlement credits")
		}
		priority, _ := new(big.Int).SetString(billing.PriorityGwei, 10)
		jobID := locked.ID
		recordID, err := processTaskCallTx(tx, RecordTaskCallInput{
			UserID:               locked.UserID,
			ProjectID:            locked.ProjectID,
			TaskJobID:            &jobID,
			TaskType:             locked.TaskType,
			APIType:              locked.APIType,
			Model:                locked.Model,
			PriorityGwei:         priority,
			TokenUsageApplicable: false,
			Status:               models.TaskCallStatusSuccess,
			Credits:              credits,
			AcceptedAt:           locked.CreatedAt,
			CompletedAt:          now,
			BilledVram:           billing.BilledVram,
			Charge:               true,
		})
		if err != nil {
			return err
		}
		if err := tx.Model(&models.TaskJob{}).Where("id = ?", locked.ID).Updates(map[string]interface{}{
			"status":              models.TaskJobStatusCompleted,
			"billing_status":      models.TaskJobBillingBilled,
			"completed_at":        now,
			"task_call_record_id": recordID,
		}).Error; err != nil {
			return err
		}
		return tx.First(job, locked.ID).Error
	})
	return job, err
}
