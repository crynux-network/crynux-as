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

const llmJobPollInterval = 2 * time.Second
const llmJobBatchLimit = 100

const weiPerGwei = int64(1_000_000_000)

func RunLLMJobWorker(ctx context.Context) {
	db := config.GetDB()
	if err := RecoverIncompleteJobs(ctx, db); err != nil {
		log.Errorf("recover incomplete llm jobs failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Infoln("llm job worker stopped")
			return
		default:
		}

		if err := advanceLLMJobs(ctx, db); err != nil {
			log.Errorf("advance llm jobs failed: %v", err)
		}

		select {
		case <-ctx.Done():
			log.Infoln("llm job worker stopped")
			return
		case <-time.After(llmJobPollInterval):
		}
	}
}

func RecoverIncompleteJobs(ctx context.Context, db *gorm.DB) error {
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var jobs []models.LLMJob
	if err := db.WithContext(dbCtx).
		Where("status IN ?", []models.LLMJobStatus{
			models.LLMJobStatusSubmitted,
			models.LLMJobStatusInProgress,
		}).
		Find(&jobs).Error; err != nil {
		return err
	}

	for _, job := range jobs {
		if job.BridgeClientTaskID == nil {
			if err := ResetLLMJobToPending(ctx, db, job.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func advanceLLMJobs(ctx context.Context, db *gorm.DB) error {
	jobs, err := ListUnfinishedLLMJobs(ctx, db, llmJobBatchLimit)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	cfg := config.GetConfig()
	client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)

	pending := make([]*models.LLMJob, 0)
	inFlight := make([]*models.LLMJob, 0)
	for i := range jobs {
		job := &jobs[i]
		if job.BridgeClientTaskID == nil {
			pending = append(pending, job)
			continue
		}
		inFlight = append(inFlight, job)
	}

	if err := submitPendingLLMJobs(ctx, db, client, pending, time.Duration(cfg.LLM.JobSubmitTimeout)*time.Second); err != nil {
		return err
	}
	return syncInFlightLLMJobs(ctx, db, client, cfg, inFlight)
}

func submitPendingLLMJobs(
	ctx context.Context,
	db *gorm.DB,
	client *bridge.RawTaskClient,
	jobs []*models.LLMJob,
	submitTimeout time.Duration,
) error {
	if len(jobs) == 0 {
		return nil
	}

	now := time.Now()
	validJobs := make([]*models.LLMJob, 0, len(jobs))
	requests := make([]bridge.CreateRawTaskRequest, 0, len(jobs))
	for _, job := range jobs {
		if isLLMJobSubmitTimedOut(job, submitTimeout, now) {
			msg := fmt.Sprintf("bridge submit timed out after %s", submitTimeout)
			if failErr := failLLMJobWithRecord(ctx, db, job, msg); failErr != nil {
				return failErr
			}
			continue
		}
		taskFeeWei, err := llmJobTaskFeeWei(job)
		if err != nil {
			if failErr := failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("bridge submit failed: %v", err)); failErr != nil {
				return failErr
			}
			continue
		}
		validJobs = append(validJobs, job)
		requests = append(requests, bridge.CreateRawTaskRequest{
			TaskArgs: job.TaskArgsJSON,
			TaskType: 1,
			MinVram:  job.BilledVram,
			TaskFee:  taskFeeWei.String(),
		})
	}
	if len(requests) == 0 {
		return nil
	}

	results, err := client.CreateLLMTasks(ctx, requests)
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
			if failErr := failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("bridge submit failed: %s", msg)); failErr != nil {
				return failErr
			}
			continue
		}
		if err := UpdateLLMJobBridgeTask(ctx, db, job.ID, result.ClientTask.ID); err != nil {
			return err
		}
	}
	return nil
}

func isLLMJobSubmitTimedOut(job *models.LLMJob, submitTimeout time.Duration, now time.Time) bool {
	if job == nil || submitTimeout <= 0 || job.CreatedAt.IsZero() {
		return false
	}
	return !now.Before(job.CreatedAt.Add(submitTimeout))
}

func syncInFlightLLMJobs(
	ctx context.Context,
	db *gorm.DB,
	client *bridge.RawTaskClient,
	cfg *config.AppConfig,
	jobs []*models.LLMJob,
) error {
	if len(jobs) == 0 {
		return nil
	}

	ids := make([]uint, 0, len(jobs))
	jobByBridgeID := make(map[uint]*models.LLMJob, len(jobs))
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
			if failErr := failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("bridge status failed: %s", msg)); failErr != nil {
				return failErr
			}
			continue
		}

		status := result.ClientTask.Status
		if !bridge.IsBridgeTaskTerminal(status) {
			if job.Status != models.LLMJobStatusInProgress {
				if err := UpdateLLMJobStatus(ctx, db, job.ID, models.LLMJobStatusInProgress); err != nil {
					return err
				}
			}
			continue
		}

		if !bridge.IsBridgeTaskSuccess(status) {
			msg := fmt.Sprintf("bridge task failed with status %s", status)
			if failErr := failLLMJobWithRecord(ctx, db, job, msg); failErr != nil {
				return failErr
			}
			continue
		}

		if err := completeSuccessfulLLMJob(ctx, db, client, job); err != nil {
			log.Errorf("complete llm job %d failed, will retry next tick: %v", job.ID, err)
			continue
		}
	}
	return nil
}

func completeSuccessfulLLMJob(
	ctx context.Context,
	db *gorm.DB,
	client *bridge.RawTaskClient,
	job *models.LLMJob,
) error {
	bridgeClientTaskID := *job.BridgeClientTaskID
	rawBytes, err := client.DownloadLLMResult(ctx, bridgeClientTaskID)
	if err != nil {
		return fmt.Errorf("bridge download failed: %w", err)
	}

	var rawResponse models.GPTTaskResponse
	if err := json.Unmarshal(rawBytes, &rawResponse); err != nil {
		return failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("invalid task result: %v", err))
	}

	formatted, err := formatLLMJobResult(job, &rawResponse)
	if err != nil {
		return failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("format result failed: %v", err))
	}

	_, err = CompleteAndSettleLLMJob(
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

func llmJobTaskFeeWei(job *models.LLMJob) (*big.Int, error) {
	if job.TaskFeeGwei == nil {
		return nil, errors.New("task fee is required")
	}
	if job.TaskFeeGwei.Sign() < 0 {
		return nil, errors.New("task fee must be non-negative")
	}
	return new(big.Int).Mul(&job.TaskFeeGwei.Int, big.NewInt(weiPerGwei)), nil
}

func formatLLMJobResult(job *models.LLMJob, raw *models.GPTTaskResponse) ([]byte, error) {
	created := job.CreatedAt.Unix()
	switch job.APIType {
	case models.LLMAPITypeChatCompletions:
		taskID := fmt.Sprintf("chatcmpl-%d", job.ID)
		return llmadapter.FormatChatCompletionsResponse(raw, taskID, created)
	case models.LLMAPITypeCompletions:
		taskID := fmt.Sprintf("cmpl-%d", job.ID)
		return llmadapter.FormatCompletionsResponse(raw, taskID, created)
	case models.LLMAPITypeResponses:
		params := responsesObjectParamsFromJob(job, llmadapter.ResponsesStatusCompleted)
		return llmadapter.FormatResponsesObject(params, raw)
	default:
		return nil, fmt.Errorf("unsupported api type %d", job.APIType)
	}
}

func responsesObjectParamsFromJob(job *models.LLMJob, status string) llmadapter.ResponsesObjectParams {
	params := llmadapter.ResponsesObjectParams{
		ID:         job.PublicID,
		Model:      job.Model,
		CreatedAt:  job.CreatedAt.Unix(),
		Status:     status,
		Background: job.Background,
	}
	if job.Status == models.LLMJobStatusFailed && job.ErrorMessage != nil {
		params.Error = &llmadapter.ResponsesAPIError{
			Message: *job.ErrorMessage,
			Type:    "server_error",
		}
	}
	return params
}

func failLLMJobWithRecord(ctx context.Context, db *gorm.DB, job *models.LLMJob, message string) error {
	_, err := FailAndRecordLLMJob(ctx, db, job, message)
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
