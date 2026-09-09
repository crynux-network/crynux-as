package llm

import (
	"context"
	"crynux_as/api/llm/vramlimit"
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

const llmJobWaitTimeout = 10 * time.Minute

type parsedLLMJobRequest struct {
	model               string
	stream              bool
	background          bool
	vramLimit           *uint64
	maxTokens           *int
	maxCompletionTokens *int
	taskArgsJSON        string
	streamIncludeUsage  bool
}

func handleLLMJobRequest(c *gin.Context, apiType models.LLMAPIType) {
	project := GetProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}
	acceptedAt := time.Now()

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		_ = recordFailedCall(c.Request.Context(), project, "", 0, acceptedAt, time.Now(), nil)
		writeClientError(c, http.StatusBadRequest, "failed to read request body")
		return
	}

	appCfg := config.GetConfig()
	parsed, err := parseLLMJobRequest(body, apiType, int(appCfg.LLM.DefaultMaxTokens))
	if err != nil {
		model := extractModelFromBody(body)
		_ = recordFailedCall(c.Request.Context(), project, model, 0, acceptedAt, time.Now(), nil)
		writeLLMAdapterError(c, err)
		return
	}

	userVram, err := vramlimit.ResolveUserVramLimit(parsed.vramLimit, c.Param("vram_limit"))
	if err != nil {
		_ = recordFailedCall(c.Request.Context(), project, parsed.model, 0, acceptedAt, time.Now(), nil)
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	effectiveVram := vramlimit.ResolveEffectiveVram(parsed.model, userVram)

	db := config.GetDB()
	account, err := loadCreditAccount(c.Request.Context(), db, project.UserID)
	if err != nil {
		log.Errorf("Error loading credit account for user %d: %v", project.UserID, err)
		writeServerError(c)
		return
	}

	estPrompt := estimatePromptTokens(body, apiType)
	maxCompletion := service.ResolveMaxCompletionTokens(parsed.maxTokens, parsed.maxCompletionTokens, appCfg.LLM.DefaultMaxTokens)
	taskFee, err := estimateTaskFeeFn(
		c.Request.Context(),
		parsed.model,
		effectiveVram,
		project.TokenRatio,
		estPrompt,
		maxCompletion,
	)
	if err != nil {
		log.Errorf("Task fee estimation failed for project %d model %s: %v", project.ID, parsed.model, err)
		_ = recordFailedCall(c.Request.Context(), project, parsed.model, effectiveVram, acceptedAt, time.Now(), nil)
		writeServerError(c)
		return
	}
	if err := service.EnsureSufficientBalance(&account.Balance.Int, taskFee.Credits); err != nil {
		_ = recordFailedCall(c.Request.Context(), project, parsed.model, effectiveVram, acceptedAt, time.Now(), taskFee)
		writeClientError(c, http.StatusPaymentRequired, "insufficient credits balance")
		return
	}

	jobInput := service.CreateLLMJobInput{
		Project:               project,
		APIType:               apiType,
		Model:                 parsed.model,
		BilledVram:            effectiveVram,
		Background:            parsed.background,
		Stream:                parsed.stream,
		RequestBody:           body,
		TaskArgsJSON:          parsed.taskArgsJSON,
		TaskFeeGwei:           taskFee.TaskFeeGwei,
		MedianPriorityGwei:    taskFee.MedianPriorityGwei,
		EstimatedNodeSeconds:  float64Ptr(taskFee.EstimatedNodeSeconds),
		VramWeight:            float64Ptr(taskFee.VramWeight),
		ConstantSeconds:       float64Ptr(taskFee.ConstantSeconds),
		SecondsPerInputToken:  float64Ptr(taskFee.SecondsPerInputToken),
		SecondsPerOutputToken: float64Ptr(taskFee.SecondsPerOutputToken),
	}

	job, err := service.CreateLLMJob(c.Request.Context(), db, jobInput)
	if err != nil {
		log.Errorf("Create LLM job failed for project %d: %v", project.ID, err)
		writeServerError(c)
		return
	}

	finalJob, err := service.WaitForLLMJob(c.Request.Context(), db, job.ID, llmJobWaitTimeout)
	if err != nil {
		if errors.Is(err, service.ErrLLMJobWaitTimeout) {
			writeClientError(c, http.StatusGatewayTimeout, "request timed out waiting for task completion")
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Errorf("Wait for LLM job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}

	if finalJob.Status == models.LLMJobStatusFailed {
		msg := "task failed"
		if finalJob.ErrorMessage != nil {
			msg = *finalJob.ErrorMessage
		}
		writeClientError(c, http.StatusInternalServerError, msg)
		return
	}

	writeCompletedLLMJobResponse(c, finalJob, parsed.streamIncludeUsage)
}

func parseLLMJobRequest(body []byte, apiType models.LLMAPIType, defaultMaxTokens int) (parsedLLMJobRequest, error) {
	switch apiType {
	case models.LLMAPITypeChatCompletions:
		taskArgsJSON, meta, err := llmadapter.BuildChatCompletionsTaskArgs(body, defaultMaxTokens)
		if err != nil {
			return parsedLLMJobRequest{}, err
		}
		includeUsage := false
		if meta.StreamOptions != nil {
			includeUsage = meta.StreamOptions.IncludeUsage
		}
		return parsedLLMJobRequest{
			model:               meta.Model,
			stream:              meta.Stream,
			vramLimit:           meta.VramLimit,
			maxTokens:           meta.MaxTokens,
			maxCompletionTokens: meta.MaxCompletionTokens,
			taskArgsJSON:        taskArgsJSON,
			streamIncludeUsage:  includeUsage,
		}, nil
	case models.LLMAPITypeCompletions:
		taskArgsJSON, meta, err := llmadapter.BuildCompletionsTaskArgs(body, defaultMaxTokens)
		if err != nil {
			return parsedLLMJobRequest{}, err
		}
		includeUsage := false
		if meta.StreamOptions != nil {
			includeUsage = meta.StreamOptions.IncludeUsage
		}
		return parsedLLMJobRequest{
			model:               meta.Model,
			stream:              meta.Stream,
			vramLimit:           meta.VramLimit,
			maxTokens:           meta.MaxTokens,
			maxCompletionTokens: meta.MaxCompletionTokens,
			taskArgsJSON:        taskArgsJSON,
			streamIncludeUsage:  includeUsage,
		}, nil
	default:
		return parsedLLMJobRequest{}, errors.New("unsupported api type")
	}
}

func estimatePromptTokens(body []byte, apiType models.LLMAPIType) uint64 {
	switch apiType {
	case models.LLMAPITypeChatCompletions:
		return service.EstimatePromptTokensFromChatBody(body)
	case models.LLMAPITypeCompletions:
		return service.EstimatePromptTokensFromCompletionsBody(body)
	default:
		return 1
	}
}

func writeCompletedLLMJobResponse(c *gin.Context, job *models.LLMJob, streamIncludeUsage bool) {
	if job.FormattedResultJSON == nil {
		writeServerError(c)
		return
	}
	responseBytes := []byte(*job.FormattedResultJSON)

	switch job.APIType {
	case models.LLMAPITypeChatCompletions:
		if job.Stream {
			if err := llmadapter.StreamChatCompletions(c, responseBytes, streamIncludeUsage); err != nil {
				log.Errorf("stream chat completion for job %d failed: %v", job.ID, err)
			}
			return
		}
	case models.LLMAPITypeCompletions:
		if job.Stream {
			if err := llmadapter.StreamCompletions(c, responseBytes, streamIncludeUsage); err != nil {
				log.Errorf("stream completion for job %d failed: %v", job.ID, err)
			}
			return
		}
	}

	c.Data(http.StatusOK, "application/json", responseBytes)
}

func writeLLMAdapterError(c *gin.Context, err error) {
	var validationErr *llmadapter.ValidationError
	if errors.As(err, &validationErr) {
		message := validationErr.Message
		if validationErr.Field != "" {
			message = validationErr.Field + ": " + validationErr.Message
		}
		writeClientError(c, http.StatusBadRequest, message)
		return
	}
	writeClientError(c, http.StatusBadRequest, err.Error())
}

func float64Ptr(v float64) *float64 {
	copied := v
	return &copied
}

func extractModelFromBody(body []byte) string {
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Model
}
