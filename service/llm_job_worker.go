package service

import (
	"context"
	"crynux_as/bridge"
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const taskJobPollInterval = 2 * time.Second
const taskJobBatchLimit = 100

const weiPerGwei = int64(1_000_000_000)

func RunTaskJobWorker(ctx context.Context) {
	db := config.GetDB()
	if err := RecoverIncompleteJobs(ctx, db); err != nil {
		log.Errorf("recover incomplete task jobs failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Infoln("task job worker stopped")
			return
		default:
		}

		if err := advanceTaskJobs(ctx, db); err != nil {
			log.Errorf("advance task jobs failed: %v", err)
		}

		select {
		case <-ctx.Done():
			log.Infoln("task job worker stopped")
			return
		case <-time.After(taskJobPollInterval):
		}
	}
}

func RecoverIncompleteJobs(ctx context.Context, db *gorm.DB) error {
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var jobs []models.TaskJob
	if err := db.WithContext(dbCtx).
		Where("status IN ?", []models.TaskJobStatus{
			models.TaskJobStatusSubmitted,
			models.TaskJobStatusInProgress,
		}).
		Find(&jobs).Error; err != nil {
		return err
	}

	for _, job := range jobs {
		if job.BridgeClientTaskID == nil {
			if err := ResetTaskJobToPending(ctx, db, job.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func advanceTaskJobs(ctx context.Context, db *gorm.DB) error {
	jobs, err := ListUnfinishedTaskJobs(ctx, db, taskJobBatchLimit)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	cfg := config.GetConfig()
	client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)

	pending := make([]*models.TaskJob, 0)
	inFlight := make([]*models.TaskJob, 0)
	for i := range jobs {
		job := &jobs[i]
		if job.BridgeClientTaskID == nil {
			pending = append(pending, job)
			continue
		}
		inFlight = append(inFlight, job)
	}

	if err := submitPendingTaskJobs(ctx, db, client, pending, time.Duration(cfg.LLM.JobSubmitTimeout)*time.Second); err != nil {
		return err
	}
	return syncInFlightTaskJobs(ctx, db, client, cfg, inFlight)
}

func submitPendingTaskJobs(
	ctx context.Context,
	db *gorm.DB,
	client *bridge.RawTaskClient,
	jobs []*models.TaskJob,
	submitTimeout time.Duration,
) error {
	if len(jobs) == 0 {
		return nil
	}

	now := time.Now()
	validJobs := make([]*models.TaskJob, 0, len(jobs))
	requests := make([]bridge.CreateRawTaskRequest, 0, len(jobs))
	for _, job := range jobs {
		if isTaskJobSubmitTimedOut(job, submitTimeout, now) {
			msg := fmt.Sprintf("bridge submit timed out after %s", submitTimeout)
			if failErr := failTaskJobWithRecord(ctx, db, job, msg); failErr != nil {
				return failErr
			}
			continue
		}
		taskFeeWei, err := taskJobTaskFeeWei(job)
		if err != nil {
			if failErr := failTaskJobWithRecord(ctx, db, job, fmt.Sprintf("bridge submit failed: %v", err)); failErr != nil {
				return failErr
			}
			continue
		}
		minVram := uint64(0)
		if job.MinVram != nil {
			minVram = *job.MinVram
		}
		validJobs = append(validJobs, job)
		requests = append(requests, bridge.CreateRawTaskRequest{
			TaskArgs:        job.TaskArgsJSON,
			TaskType:        int(job.TaskType),
			TaskVersion:     job.TaskVersion,
			MinVram:         minVram,
			RequiredGPU:     job.RequiredGPU,
			RequiredGPUVram: job.RequiredGPUVram,
			TaskFee:         taskFeeWei.String(),
		})
	}
	if len(requests) == 0 {
		return nil
	}

	results, err := client.CreateTasks(ctx, requests)
	if err != nil {
		log.Errorf("bridge batch create failed, will retry next tick: %v", err)
		return nil
	}

	for _, result := range results {
		if result.Index < 0 || result.Index >= len(validJobs) {
			log.Errorf("bridge batch create returned out-of-range index %d", result.Index)
			continue
		}
		job := validJobs[result.Index]
		if result.Error != "" || result.ClientTask == nil {
			msg := result.Error
			if msg == "" {
				msg = "bridge batch create returned empty client task"
			}
			if failErr := failTaskJobWithRecord(ctx, db, job, fmt.Sprintf("bridge submit failed: %s", msg)); failErr != nil {
				return failErr
			}
			continue
		}
		if err := UpdateTaskJobBridgeTask(ctx, db, job.ID, result.ClientTask.ID); err != nil {
			return err
		}
	}
	return nil
}

func isTaskJobSubmitTimedOut(job *models.TaskJob, submitTimeout time.Duration, now time.Time) bool {
	if job == nil || submitTimeout <= 0 || job.CreatedAt.IsZero() {
		return false
	}
	return !now.Before(job.CreatedAt.Add(submitTimeout))
}

func syncInFlightTaskJobs(
	ctx context.Context,
	db *gorm.DB,
	client *bridge.RawTaskClient,
	cfg *config.AppConfig,
	jobs []*models.TaskJob,
) error {
	if len(jobs) == 0 {
		return nil
	}

	ids := make([]uint, 0, len(jobs))
	jobByBridgeID := make(map[uint]*models.TaskJob, len(jobs))
	for _, job := range jobs {
		bridgeID := *job.BridgeClientTaskID
		ids = append(ids, bridgeID)
		jobByBridgeID[bridgeID] = job
	}

	results, err := client.GetTaskStatuses(ctx, ids)
	if err != nil {
		return err
	}

	for _, result := range results {
		job, ok := jobByBridgeID[result.ClientTaskID]
		if !ok {
			continue
		}
		if result.Error != "" || result.ClientTask == nil {
			msg := result.Error
			if msg == "" {
				msg = "bridge batch status returned empty client task"
			}
			if failErr := failTaskJobWithRecord(ctx, db, job, fmt.Sprintf("bridge status failed: %s", msg)); failErr != nil {
				return failErr
			}
			continue
		}

		status := result.ClientTask.Status
		if !bridge.IsBridgeTaskTerminal(status) {
			if job.Status != models.TaskJobStatusInProgress {
				if err := UpdateTaskJobStatus(ctx, db, job.ID, models.TaskJobStatusInProgress); err != nil {
					return err
				}
			}
			continue
		}

		if !bridge.IsBridgeTaskSuccess(status) {
			msg := fmt.Sprintf("bridge task failed with status %s", status)
			if failErr := failTaskJobWithRecord(ctx, db, job, msg); failErr != nil {
				return failErr
			}
			continue
		}

		if err := completeSuccessfulTaskJob(ctx, db, client, job); err != nil {
			log.Errorf("complete task job %d failed, will retry next tick: %v", job.ID, err)
			continue
		}
	}
	return nil
}

func completeSuccessfulTaskJob(
	ctx context.Context,
	db *gorm.DB,
	client *bridge.RawTaskClient,
	job *models.TaskJob,
) error {
	if job.TaskType == models.TaskTypeImage {
		_, err := CompleteAndSettleImageTaskJob(ctx, db, job)
		return err
	}
	bridgeClientTaskID := *job.BridgeClientTaskID
	rawBytes, err := client.DownloadLLMResult(ctx, bridgeClientTaskID)
	if err != nil {
		return fmt.Errorf("bridge download failed: %w", err)
	}

	var rawResponse models.GPTTaskResponse
	if err := json.Unmarshal(rawBytes, &rawResponse); err != nil {
		return failTaskJobWithRecord(ctx, db, job, fmt.Sprintf("invalid task result: %v", err))
	}

	formatted, err := formatTaskJobResult(job, &rawResponse)
	if err != nil {
		return failTaskJobWithRecord(ctx, db, job, fmt.Sprintf("format result failed: %v", err))
	}

	_, err = CompleteAndSettleTaskJob(
		ctx,
		db,
		job,
		string(rawBytes),
		string(formatted),
		uint64(rawResponse.Usage.PromptTokens),
		uint64(rawResponse.Usage.CompletionTokens),
		uint64(rawResponse.Usage.TotalTokens),
	)
	return err
}

func taskJobTaskFeeWei(job *models.TaskJob) (*big.Int, error) {
	if job.BillingData == "" && job.TaskFeeGwei != nil {
		if job.TaskFeeGwei.Sign() < 0 {
			return nil, errors.New("task fee must be non-negative")
		}
		return new(big.Int).Mul(&job.TaskFeeGwei.Int, big.NewInt(1_000_000_000)), nil
	}
	billing, err := job.DecodeBillingData()
	if err != nil {
		return nil, err
	}
	fee, ok := new(big.Int).SetString(billing.TaskFeeWei, 10)
	if !ok || fee.Sign() < 0 {
		return nil, errors.New("task fee must be a non-negative integer")
	}
	return fee, nil
}

func formatTaskJobResult(job *models.TaskJob, raw *models.GPTTaskResponse) ([]byte, error) {
	created := job.CreatedAt.Unix()
	var taskArgs models.GPTTaskArgs
	if err := json.Unmarshal([]byte(job.TaskArgsJSON), &taskArgs); err != nil {
		return nil, fmt.Errorf("invalid persisted task args: %w", err)
	}
	switch job.APIType {
	case models.TaskAPITypeRaw:
		return json.Marshal(raw)
	case models.TaskAPITypeChatCompletions:
		taskID := fmt.Sprintf("chatcmpl-%d", job.ID)
		return llmadapter.FormatChatCompletionsResponseWithTaskArgs(raw, taskID, created, &taskArgs)
	case models.TaskAPITypeCompletions:
		taskID := fmt.Sprintf("cmpl-%d", job.ID)
		return llmadapter.FormatCompletionsResponse(raw, taskID, created)
	case models.TaskAPITypeResponses:
		params := responsesObjectParamsFromJob(job, llmadapter.ResponsesStatusCompleted)
		return llmadapter.FormatResponsesObjectWithTaskArgs(params, raw, &taskArgs)
	default:
		return nil, fmt.Errorf("unsupported api type %d", job.APIType)
	}
}

func responsesObjectParamsFromJob(job *models.TaskJob, status string) llmadapter.ResponsesObjectParams {
	params := llmadapter.ResponsesObjectParams{
		ID:         job.PublicID,
		Model:      job.Model,
		CreatedAt:  job.CreatedAt.Unix(),
		Status:     status,
		Background: job.Background,
	}
	if job.Status == models.TaskJobStatusFailed && job.ErrorMessage != nil {
		params.Error = &llmadapter.ResponsesAPIError{
			Message: *job.ErrorMessage,
			Type:    "server_error",
		}
	}
	return params
}

func failTaskJobWithRecord(ctx context.Context, db *gorm.DB, job *models.TaskJob, message string) error {
	_, err := FailAndRecordTaskJob(ctx, db, job, message)
	return err
}

func loadProjectByID(ctx context.Context, db *gorm.DB, projectID uint) (*models.Project, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var project models.Project
	if err := db.WithContext(dbCtx).First(&project, projectID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("project not found")
		}
		return nil, err
	}
	return &project, nil
}
