package responses

import (
	"context"
	"crynux_as/config"
	"crynux_as/llmadapter"
	"crynux_as/models"
	"crynux_as/service"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

const (
	projectContextKey  = "llm_project"
	llmJobWaitTimeout  = 10 * time.Minute
)

var estimateTaskFeeFn = service.EstimateTaskFee

func CreateResponse(c *gin.Context) {
	project := getProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeClientError(c, http.StatusBadRequest, "failed to read request body")
		return
	}

	appCfg := config.GetConfig()
	req, err := llmadapter.ParseResponsesRequest(body)
	if err != nil {
		writeLLMAdapterError(c, err)
		return
	}
	taskArgsJSON, err := llmadapter.BuildResponsesTaskArgs(req, int(appCfg.LLM.DefaultMaxTokens))
	if err != nil {
		writeLLMAdapterError(c, err)
		return
	}

	userVram, err := resolveUserVramLimit(req.VramLimit, c.Param("vram_limit"))
	if err != nil {
		writeClientError(c, http.StatusBadRequest, err.Error())
		return
	}
	effectiveVram := resolveEffectiveVram(req.Model, userVram)

	db := config.GetDB()
	account, err := loadCreditAccount(c.Request.Context(), db, project.UserID)
	if err != nil {
		log.Errorf("Error loading credit account for user %d: %v", project.UserID, err)
		writeServerError(c)
		return
	}

	estPrompt := estimatePromptTokensFromResponsesBody(body)
	maxCompletion := service.ResolveMaxCompletionTokens(nil, req.MaxOutputTokens, appCfg.LLM.DefaultMaxTokens)
	taskFee, err := estimateTaskFeeFn(
		c.Request.Context(),
		req.Model,
		effectiveVram,
		project.TokenRatio,
		estPrompt,
		maxCompletion,
	)
	if err != nil {
		log.Errorf("Task fee estimation failed for project %d model %s: %v", project.ID, req.Model, err)
		_ = recordFailedCall(c.Request.Context(), project, req.Model, effectiveVram, 0, nil)
		writeServerError(c)
		return
	}
	if err := service.EnsureSufficientBalance(&account.Balance.Int, taskFee.Credits); err != nil {
		writeClientError(c, http.StatusPaymentRequired, "insufficient credits balance")
		return
	}

	jobInput := service.CreateLLMJobInput{
		Project:               project,
		APIType:               models.LLMAPITypeResponses,
		Model:                 req.Model,
		BilledVram:            effectiveVram,
		Background:            req.Background,
		RequestBody:           body,
		TaskArgsJSON:          taskArgsJSON,
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

	if req.Background {
		writeResponsesJob(c, job, llmadapter.ResponsesStatusQueued)
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
		payload, err := loadResponsesPayload(finalJob)
		if err != nil {
			log.Errorf("format failed response job %d: %v", finalJob.ID, err)
			writeServerError(c)
			return
		}
		c.Data(http.StatusOK, "application/json", payload)
		return
	}

	if finalJob.FormattedResultJSON == nil {
		writeServerError(c)
		return
	}
	c.Data(http.StatusOK, "application/json", []byte(*finalJob.FormattedResultJSON))
}

func GetResponse(c *gin.Context) {
	project := getProject(c)
	if project == nil {
		writeAuthError(c, "unauthorized")
		return
	}

	publicID := strings.TrimSpace(c.Param("response_id"))
	if publicID == "" {
		writeClientError(c, http.StatusBadRequest, "response id is required")
		return
	}

	db := config.GetDB()
	job, err := service.GetLLMJobByPublicID(c.Request.Context(), db, project.ID, publicID)
	if err != nil {
		if errors.Is(err, service.ErrLLMJobNotFound) {
			writeClientError(c, http.StatusNotFound, "response not found")
			return
		}
		log.Errorf("load response job for project %d public_id %s: %v", project.ID, publicID, err)
		writeServerError(c)
		return
	}
	if job.APIType != models.LLMAPITypeResponses {
		writeClientError(c, http.StatusNotFound, "response not found")
		return
	}

	payload, err := loadResponsesPayload(job)
	if err != nil {
		log.Errorf("format response job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}

	c.Data(http.StatusOK, "application/json", payload)
}

func getProject(c *gin.Context) *models.Project {
	v, ok := c.Get(projectContextKey)
	if !ok {
		return nil
	}
	project, _ := v.(*models.Project)
	return project
}

func writeAuthError(c *gin.Context, message string) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func writeClientError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
		},
	})
}

func writeServerError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{
		"error": gin.H{
			"message": "internal server error",
			"type":    "server_error",
		},
	})
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

func writeResponsesJob(c *gin.Context, job *models.LLMJob, status string) {
	payload, err := llmadapter.FormatResponsesPendingObject(llmadapter.ResponsesObjectParams{
		ID:         job.PublicID,
		Model:      job.Model,
		CreatedAt:  job.CreatedAt.Unix(),
		Status:     status,
		Background: job.Background,
	})
	if err != nil {
		log.Errorf("format pending response for job %d failed: %v", job.ID, err)
		writeServerError(c)
		return
	}
	c.Data(http.StatusOK, "application/json", payload)
}

func loadResponsesPayload(job *models.LLMJob) ([]byte, error) {
	if job.Status == models.LLMJobStatusCompleted && job.FormattedResultJSON != nil {
		return []byte(*job.FormattedResultJSON), nil
	}
	params := responsesObjectParamsFromJob(job)
	if job.Status == models.LLMJobStatusFailed {
		return llmadapter.FormatResponsesObject(params, nil)
	}
	return llmadapter.FormatResponsesPendingObject(params)
}

func responsesObjectParamsFromJob(job *models.LLMJob) llmadapter.ResponsesObjectParams {
	status := llmadapter.ResponsesStatusQueued
	switch job.Status {
	case models.LLMJobStatusSubmitted, models.LLMJobStatusInProgress:
		status = llmadapter.ResponsesStatusInProgress
	case models.LLMJobStatusCompleted:
		status = llmadapter.ResponsesStatusCompleted
	case models.LLMJobStatusFailed:
		status = llmadapter.ResponsesStatusFailed
	}

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

func estimatePromptTokensFromResponsesBody(body []byte) uint64 {
	req, err := llmadapter.ParseResponsesRequest(body)
	if err != nil {
		return 1
	}
	promptBody, err := json.Marshal(map[string]any{
		"instructions": req.Instructions,
		"input":        req.Input,
	})
	if err != nil {
		return 1
	}
	return service.EstimatePromptTokensFromChatBody(promptBody)
}

func float64Ptr(v float64) *float64 {
	copied := v
	return &copied
}

func resolveUserVramLimit(bodyVramLimit *uint64, pathVramLimit string) (*uint64, error) {
	trimmedPathVramLimit := strings.TrimSpace(pathVramLimit)
	if trimmedPathVramLimit != "" {
		return nil, errors.New("vram_limit path segment is not supported for responses")
	}
	return bodyVramLimit, nil
}

func resolveEffectiveVram(model string, userVram *uint64) uint64 {
	if userVram != nil {
		return *userVram
	}
	if loaded, ok := service.GetLoadedLLMModel(model); ok {
		return loaded.MinVRAM
	}
	return config.GetConfig().LLM.DefaultVramLimit
}

func loadCreditAccount(ctx context.Context, db *gorm.DB, userID uint) (*models.CreditAccount, error) {
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var account models.CreditAccount
	if err := db.WithContext(dbCtx).Where("user_id = ?", userID).First(&account).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func recordFailedCall(ctx context.Context, project *models.Project, model string, billedVram uint64, duration time.Duration, taskFee *service.CalcTaskFeeResult) error {
	in := service.RecordLLMCallInput{
		UserID:     project.UserID,
		ProjectID:  project.ID,
		Model:      model,
		TokenRatio: project.TokenRatio,
		Status:     models.LLMCallStatusFailed,
		Credits:    big.NewInt(0),
		DurationMs: uint64(duration.Milliseconds()),
		BilledVram: billedVram,
		Charge:     false,
	}
	if taskFee != nil {
		in.TaskFeeGwei = taskFee.TaskFeeGwei
		in.MedianPriorityGwei = taskFee.MedianPriorityGwei
		estimated := taskFee.EstimatedNodeSeconds
		weight := taskFee.VramWeight
		in.EstimatedNodeSeconds = &estimated
		in.VramWeight = &weight
	}
	_, err := service.ProcessLLMCall(ctx, config.GetDB(), in)
	return err
}
