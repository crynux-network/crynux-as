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

		job, err := ClaimNextLLMJob(ctx, db)
		if err != nil {
			log.Errorf("claim llm job failed: %v", err)
			time.Sleep(llmJobPollInterval)
			continue
		}
		if job == nil {
			time.Sleep(llmJobPollInterval)
			continue
		}

		if err := processLLMJob(ctx, db, job); err != nil {
			log.Errorf("process llm job %d failed: %v", job.ID, err)
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

func processLLMJob(ctx context.Context, db *gorm.DB, job *models.LLMJob) error {
	cfg := config.GetConfig()
	client := bridge.NewRawTaskClient(cfg.Bridge.BaseURL, cfg.Bridge.APIKey)

	if job.BridgeClientTaskID == nil {
		requestID := fmt.Sprintf("as-job-%d", job.ID)
		rawTask, err := client.CreateLLMTask(ctx, requestID, job.TaskArgsJSON, job.BilledVram)
		if err != nil {
			return failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("bridge submit failed: %v", err))
		}
		if err := UpdateLLMJobBridgeTask(ctx, db, job.ID, rawTask.ID); err != nil {
			return err
		}
		job.BridgeClientTaskID = &rawTask.ID
	}

	bridgeClientTaskID := *job.BridgeClientTaskID
	for {
		status, err := client.GetTaskStatus(ctx, bridgeClientTaskID)
		if err != nil {
			return failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("bridge status failed: %v", err))
		}

		if !bridge.IsBridgeTaskTerminal(status.Status) {
			if job.Status != models.LLMJobStatusInProgress {
				if err := UpdateLLMJobStatus(ctx, db, job.ID, models.LLMJobStatusInProgress); err != nil {
					return err
				}
				job.Status = models.LLMJobStatusInProgress
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(llmJobPollInterval):
			}
			continue
		}

		if !bridge.IsBridgeTaskSuccess(status.Status) {
			msg := fmt.Sprintf("bridge task failed with status %d", status.Status)
			return failLLMJobWithRecord(ctx, db, job, msg)
		}

		rawBytes, err := client.DownloadLLMResult(ctx, bridgeClientTaskID, 0)
		if err != nil {
			return failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("bridge download failed: %v", err))
		}

		var rawResponse models.GPTTaskResponse
		if err := json.Unmarshal(rawBytes, &rawResponse); err != nil {
			return failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("invalid task result: %v", err))
		}

		formatted, err := formatLLMJobResult(job, &rawResponse)
		if err != nil {
			return failLLMJobWithRecord(ctx, db, job, fmt.Sprintf("format result failed: %v", err))
		}

		rawJSON := string(rawBytes)
		completed, err := CompleteLLMJob(
			ctx,
			db,
			job.ID,
			rawJSON,
			string(formatted),
			uint64(rawResponse.Usage.PromptTokens),
			uint64(rawResponse.Usage.CompletionTokens),
			uint64(rawResponse.Usage.TotalTokens),
		)
		if err != nil {
			return err
		}

		project, err := loadProjectByID(ctx, db, completed.ProjectID)
		if err != nil {
			return err
		}

		vramRatio := SelectVramRatio(cfg.LLM.VramRatios, completed.BilledVram)
		prices := LLMPrices{
			PromptCreditsPerToken:     cfg.LLM.PromptCreditsPerToken,
			CompletionCreditsPerToken: cfg.LLM.CompletionCreditsPerToken,
		}
		if err := SettleLLMJob(ctx, db, completed, project, vramRatio, prices); err != nil {
			return err
		}
		return nil
	}
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
	if _, err := FailLLMJob(ctx, db, job.ID, message); err != nil {
		return err
	}

	project, err := loadProjectByID(ctx, db, job.ProjectID)
	if err != nil {
		return err
	}

	durationMs := uint64(0)
	if job.StartedAt != nil {
		durationMs = uint64(time.Since(*job.StartedAt).Milliseconds())
	}

	in := RecordLLMCallInput{
		UserID:     project.UserID,
		ProjectID:  project.ID,
		Model:      job.Model,
		TokenRatio: project.TokenRatio,
		Status:     models.LLMCallStatusFailed,
		Credits:    big.NewInt(0),
		DurationMs: durationMs,
		BilledVram: job.BilledVram,
		Charge:     false,
	}
	if job.TaskFeeGwei != nil {
		in.TaskFeeGwei = &job.TaskFeeGwei.Int
	}
	if job.MedianPriorityGwei != nil {
		in.MedianPriorityGwei = &job.MedianPriorityGwei.Int
	}
	in.EstimatedNodeSeconds = cloneFloat64Ptr(job.EstimatedNodeSeconds)
	in.VramWeight = cloneFloat64Ptr(job.VramWeight)

	_, err = ProcessLLMCall(ctx, db, in)
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
