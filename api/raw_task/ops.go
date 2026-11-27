package raw_task

import (
	"context"
	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/models"
	"crynux_as/service"
	"errors"
	"fmt"
	"net/http"

	"gorm.io/gorm"
)

var (
	ErrTaskNotFound      = errors.New("task not found")
	ErrInsufficientCreds = service.ErrInsufficientBalance
	ErrInvalidBatchSize  = errors.New("batch size must be between 1 and 100")
	ErrTaskNotLLM        = errors.New("task is not an LLM task")
	ErrTaskNotSuccess    = errors.New("task is not successful")
)

type CreateOutcome struct {
	View           *TaskView
	ClientTaskID   uint
	ModelNotReady  bool
	ErrorMessage   string
	HTTPStatusHint int
}

// CreateForProject creates one Raw Task for the project.
func CreateForProject(ctx context.Context, project *models.Project, request CreateTaskRequest) (*CreateOutcome, error) {
	prepared, err := prepareTask(ctx, project, request)
	if err != nil {
		return &CreateOutcome{ErrorMessage: err.Error(), HTTPStatusHint: http.StatusBadRequest}, nil
	}
	job, err := createPreparedTask(ctx, project, prepared)
	if err != nil {
		if errors.Is(err, service.ErrInsufficientBalance) {
			return &CreateOutcome{ErrorMessage: "insufficient credits balance", HTTPStatusHint: http.StatusPaymentRequired}, nil
		}
		return nil, err
	}
	if !rawTaskModelReady(prepared) {
		view := taskView(job)
		return &CreateOutcome{
			View:           &view,
			ClientTaskID:   job.ID,
			ModelNotReady:  true,
			ErrorMessage:   service.ModelNotReadyMessage(),
			HTTPStatusHint: http.StatusBadRequest,
		}, nil
	}
	view := taskView(job)
	return &CreateOutcome{View: &view, ClientTaskID: job.ID, HTTPStatusHint: http.StatusOK}, nil
}

// BatchCreateForProject creates up to 100 Raw Tasks.
func BatchCreateForProject(ctx context.Context, project *models.Project, tasks []CreateTaskRequest) ([]BatchCreateItem, bool, error) {
	if len(tasks) == 0 || len(tasks) > maxBatchSize {
		return nil, true, ErrInvalidBatchSize
	}
	results := make([]BatchCreateItem, 0, len(tasks))
	hasError := false
	for index, item := range tasks {
		outcome, err := CreateForProject(ctx, project, item)
		if err != nil {
			hasError = true
			results = append(results, BatchCreateItem{Index: index, Error: "failed to create task"})
			continue
		}
		if outcome.ErrorMessage != "" && outcome.View == nil {
			hasError = true
			results = append(results, BatchCreateItem{Index: index, Error: outcome.ErrorMessage})
			continue
		}
		if outcome.ModelNotReady {
			hasError = true
			id := outcome.ClientTaskID
			results = append(results, BatchCreateItem{
				Index:        index,
				Error:        outcome.ErrorMessage,
				ClientTaskID: &id,
			})
			continue
		}
		results = append(results, BatchCreateItem{Index: index, ClientTask: outcome.View})
	}
	return results, hasError, nil
}

// GetForProject returns one task view owned by the project.
func GetForProject(ctx context.Context, projectID, id uint) (*TaskView, error) {
	job, err := loadProjectTask(ctx, projectID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	view := taskView(job)
	return &view, nil
}

// BatchStatusForProject returns status views for the requested IDs.
func BatchStatusForProject(ctx context.Context, projectID uint, ids []uint) ([]BatchStatusItem, error) {
	if len(ids) == 0 || len(ids) > maxBatchSize {
		return nil, ErrInvalidBatchSize
	}
	unique := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	var jobs []models.TaskJob
	if err := config.GetDB().WithContext(ctx).
		Where("project_id = ? AND id IN ?", projectID, unique).
		Find(&jobs).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]*models.TaskJob, len(jobs))
	for i := range jobs {
		byID[jobs[i].ID] = &jobs[i]
	}
	results := make([]BatchStatusItem, 0, len(ids))
	for _, id := range ids {
		job := byID[id]
		if job == nil {
			results = append(results, BatchStatusItem{ClientTaskID: id, Error: "task not found"})
			continue
		}
		view := taskView(job)
		results = append(results, BatchStatusItem{ClientTaskID: id, ClientTask: &view})
	}
	return results, nil
}

// AbortForProject aborts one Raw Task.
func AbortForProject(ctx context.Context, projectID, id uint) (*TaskView, error) {
	job, err := service.AbortRawTaskJob(ctx, config.GetDB(), projectID, id)
	if err != nil {
		return nil, err
	}
	view := taskView(job)
	return &view, nil
}

// BatchAbortForProject aborts many Raw Tasks.
func BatchAbortForProject(ctx context.Context, projectID uint, ids []uint) ([]BatchAbortItem, error) {
	if len(ids) == 0 || len(ids) > maxBatchSize {
		return nil, ErrInvalidBatchSize
	}
	items, err := service.AbortRawTaskJobs(ctx, config.GetDB(), projectID, ids)
	if err != nil {
		return nil, err
	}
	results := make([]BatchAbortItem, 0, len(items))
	for _, item := range items {
		if item.Error != "" {
			results = append(results, BatchAbortItem{ClientTaskID: item.ClientTaskID, Error: item.Error})
			continue
		}
		view := taskView(item.Job)
		results = append(results, BatchAbortItem{ClientTaskID: item.ClientTaskID, ClientTask: &view})
	}
	return results, nil
}

// GetLLMResultForProject returns the raw LLM result JSON for a successful LLM job.
func GetLLMResultForProject(ctx context.Context, projectID, id uint) ([]byte, error) {
	job, err := loadProjectTask(ctx, projectID, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	if job.TaskType != models.TaskTypeLLM {
		return nil, ErrTaskNotLLM
	}
	if job.Status != models.TaskJobStatusCompleted || job.BridgeClientTaskID == nil {
		return nil, ErrTaskNotSuccess
	}
	cfg := config.GetConfig()
	client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)
	body, err := client.DownloadLLMResult(ctx, *job.BridgeClientTaskID)
	if err != nil {
		return nil, fmt.Errorf("download llm result: %w", err)
	}
	return body, nil
}
