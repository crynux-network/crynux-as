package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/models"

	"gorm.io/gorm"
)

const RawTaskAbortErrorMessage = "cancelled by client"

var (
	ErrRawTaskAbortNotFound        = errors.New("task not found")
	ErrRawTaskAbortNotCancellable  = errors.New("not_cancellable")
	ErrRawTaskAbortTemporary       = errors.New("temporary_error")
	ErrRawTaskAbortAlreadyComplete = errors.New("already_completed")
)

var abortBridgeClientTask = func(ctx context.Context, bridgeClientTaskID uint) error {
	cfg := config.GetConfig()
	client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)
	_, err := client.AbortTask(ctx, bridgeClientTaskID)
	return err
}

func AbortRawTaskJob(ctx context.Context, db *gorm.DB, projectID, jobID uint) (*models.TaskJob, error) {
	job, err := loadProjectRawTaskJobForAbort(ctx, db, projectID, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status == models.TaskJobStatusFailed && job.ErrorMessage != nil && *job.ErrorMessage == RawTaskAbortErrorMessage {
		return job, nil
	}
	if job.Status == models.TaskJobStatusCompleted {
		return nil, ErrRawTaskAbortAlreadyComplete
	}
	if job.IsTerminal() {
		return nil, ErrRawTaskAbortNotCancellable
	}

	if job.BridgeClientTaskID == nil {
		return FailAndRecordTaskJob(ctx, db, job, RawTaskAbortErrorMessage)
	}

	if err := abortBridgeClientTask(ctx, *job.BridgeClientTaskID); err != nil {
		return nil, mapBridgeAbortError(err)
	}

	job, err = loadProjectRawTaskJobForAbort(ctx, db, projectID, jobID)
	if err != nil {
		return nil, err
	}
	if job.Status == models.TaskJobStatusCompleted {
		return nil, ErrRawTaskAbortAlreadyComplete
	}
	if job.Status == models.TaskJobStatusFailed && job.ErrorMessage != nil && *job.ErrorMessage == RawTaskAbortErrorMessage {
		return job, nil
	}
	return FailAndRecordTaskJob(ctx, db, job, RawTaskAbortErrorMessage)
}

type AbortRawTaskBatchItem struct {
	ClientTaskID uint
	Job          *models.TaskJob
	Error        string
}

func AbortRawTaskJobs(ctx context.Context, db *gorm.DB, projectID uint, jobIDs []uint) ([]AbortRawTaskBatchItem, error) {
	results := make([]AbortRawTaskBatchItem, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		item := AbortRawTaskBatchItem{ClientTaskID: jobID}
		job, err := AbortRawTaskJob(ctx, db, projectID, jobID)
		if err != nil {
			item.Error = rawTaskAbortErrorString(err)
		} else {
			item.Job = job
		}
		results = append(results, item)
	}
	return results, nil
}

func loadProjectRawTaskJobForAbort(ctx context.Context, db *gorm.DB, projectID, jobID uint) (*models.TaskJob, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var job models.TaskJob
	err := db.WithContext(dbCtx).
		Where("project_id = ? AND id = ? AND api_type = ?", projectID, jobID, models.TaskAPITypeRaw).
		First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRawTaskAbortNotFound
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func mapBridgeAbortError(err error) error {
	switch {
	case errors.Is(err, bridge.ErrAbortNotCancellable):
		return ErrRawTaskAbortNotCancellable
	case errors.Is(err, bridge.ErrAbortNotFound):
		return ErrRawTaskAbortNotCancellable
	case errors.Is(err, bridge.ErrAbortTemporary):
		return fmt.Errorf("%w: %v", ErrRawTaskAbortTemporary, err)
	default:
		return fmt.Errorf("%w: %v", ErrRawTaskAbortTemporary, err)
	}
}

func rawTaskAbortErrorString(err error) string {
	switch {
	case errors.Is(err, ErrRawTaskAbortNotFound):
		return "task not found"
	case errors.Is(err, ErrRawTaskAbortAlreadyComplete), errors.Is(err, ErrRawTaskAbortNotCancellable):
		return "not_cancellable"
	case errors.Is(err, ErrRawTaskAbortTemporary):
		return "temporary_error"
	default:
		return "temporary_error"
	}
}
